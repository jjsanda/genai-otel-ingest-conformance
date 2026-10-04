"""LangChain callbacks → OpenTelemetry GenAI semantic conventions.

This is the bridge the demo exists for. LangChain/LangGraph announce their
work through callback events (``on_chat_model_start``, ``on_tool_start``,
...); OpenTelemetry wants spans and metrics shaped by the GenAI semantic
conventions. The ``GenAISpanBridge`` below maps one onto the other in ~200
lines, pinned to the same conventions snapshot the conformance suite
enforces (see internal/registry/curated/ in the repo root).

Design notes, because they are the point:

- **Explicit parenting, no ambient context.** LangGraph may run graph nodes
  on worker threads, where OpenTelemetry's context variable does not follow.
  Every span here is created with an explicit parent ``Context``: the
  LangChain ``parent_run_id`` when we track it, otherwise the enclosing
  ``invoke_agent`` span the runner registered. Nothing depends on what
  thread a callback fires on.

- **One bridge per agent invocation.** The bridge accumulates token usage
  and finish reasons for the wrapping ``invoke_agent`` span; a fresh
  instance per invocation keeps that state trivially correct.

- **Span kinds follow the conventions, not intuition.** ``chat`` spans are
  CLIENT (a call to a model service), ``execute_tool`` spans are INTERNAL
  (the tool runs in-process), and the retrieval node maps to a CLIENT
  ``retrieval {data source}`` span per the retrieval span convention.
"""

from __future__ import annotations

import threading
import time
from dataclasses import dataclass
from typing import Any
from uuid import UUID

from langchain_core.callbacks import BaseCallbackHandler
from langchain_core.messages import BaseMessage
from langchain_core.outputs import LLMResult
from opentelemetry.context import Context
from opentelemetry.metrics import Histogram
from opentelemetry.trace import Span, SpanKind, StatusCode, Tracer, set_span_in_context

# Attribute names, transcribed from the pinned conventions. Spelled out as
# constants so a rename upstream is one grep away.
OPERATION_NAME = "gen_ai.operation.name"
PROVIDER_NAME = "gen_ai.provider.name"
REQUEST_MODEL = "gen_ai.request.model"
RESPONSE_MODEL = "gen_ai.response.model"
RESPONSE_ID = "gen_ai.response.id"
FINISH_REASONS = "gen_ai.response.finish_reasons"
INPUT_TOKENS = "gen_ai.usage.input_tokens"
OUTPUT_TOKENS = "gen_ai.usage.output_tokens"
TOKEN_TYPE = "gen_ai.token.type"
AGENT_NAME = "gen_ai.agent.name"
TOOL_NAME = "gen_ai.tool.name"
TOOL_CALL_ID = "gen_ai.tool.call.id"
TOOL_TYPE = "gen_ai.tool.type"
TOOL_DESCRIPTION = "gen_ai.tool.description"
DATA_SOURCE_ID = "gen_ai.data_source.id"
RETRIEVAL_TOP_K = "gen_ai.retrieval.top_k"
ERROR_TYPE = "error.type"
SERVER_ADDRESS = "server.address"
SERVER_PORT = "server.port"

# The graph node name the bridge maps to a `retrieval {data source}` span.
RETRIEVAL_NODE_NAME = "retrieval"


@dataclass
class _Run:
    span: Span
    started: float


class GenAISpanBridge(BaseCallbackHandler):
    """Maps LangChain callback events to GenAI-semconv spans and metrics."""

    def __init__(
        self,
        *,
        tracer: Tracer,
        token_usage: Histogram,
        operation_duration: Histogram,
        provider: str,
        request_model: str,
        agent_name: str,
        data_source_id: str,
        retrieval_top_k: int,
        server_address: str | None = None,
    ) -> None:
        self._tracer = tracer
        self._token_usage = token_usage
        self._operation_duration = operation_duration
        self._provider = provider
        self._request_model = request_model
        self._agent_name = agent_name
        self._data_source_id = data_source_id
        self._retrieval_top_k = retrieval_top_k
        self._server_address = server_address

        self._lock = threading.Lock()
        self._runs: dict[UUID, _Run] = {}

        # Set by the runner to the invoke_agent span's context; every span
        # whose LangChain parent run we do not track parents here.
        self.root_context: Context | None = None

        # Rollup for the wrapping invoke_agent span.
        self.total_input_tokens = 0
        self.total_output_tokens = 0
        self.finish_reasons: list[str] = []

    # ------------------------------------------------------------- helpers

    def _parent_context(self, parent_run_id: UUID | None) -> Context | None:
        with self._lock:
            if parent_run_id is not None and parent_run_id in self._runs:
                return set_span_in_context(self._runs[parent_run_id].span)
        return self.root_context

    def _track(self, run_id: UUID, span: Span) -> None:
        with self._lock:
            self._runs[run_id] = _Run(span=span, started=time.monotonic())

    def _pop(self, run_id: UUID) -> _Run | None:
        with self._lock:
            return self._runs.pop(run_id, None)

    def _metric_attrs(self, response_model: str | None) -> dict[str, str]:
        # provider.name is required on gen_ai.client.token.usage and
        # conditionally required on operation.duration (this IS a provider
        # call), so both carry it.
        attrs = {
            OPERATION_NAME: "chat",
            PROVIDER_NAME: self._provider,
            REQUEST_MODEL: self._request_model,
        }
        if response_model:
            attrs[RESPONSE_MODEL] = response_model
        return attrs

    # ----------------------------------------------------------- chat model

    def on_chat_model_start(
        self,
        serialized: dict[str, Any],
        messages: list[list[BaseMessage]],
        *,
        run_id: UUID,
        parent_run_id: UUID | None = None,
        **kwargs: Any,
    ) -> None:
        # Span name format for inference spans: "{operation} {request model}".
        span = self._tracer.start_span(
            f"chat {self._request_model}",
            context=self._parent_context(parent_run_id),
            kind=SpanKind.CLIENT,
        )
        span.set_attribute(OPERATION_NAME, "chat")
        span.set_attribute(PROVIDER_NAME, self._provider)
        span.set_attribute(REQUEST_MODEL, self._request_model)
        if self._server_address:
            # server.port is conditionally REQUIRED once server.address is
            # set — emitting the address alone is a conformance violation.
            span.set_attribute(SERVER_ADDRESS, self._server_address)
            span.set_attribute(SERVER_PORT, 443)
        self._track(run_id, span)

    def on_llm_end(self, response: LLMResult, *, run_id: UUID, **kwargs: Any) -> None:
        run = self._pop(run_id)
        if run is None:
            return
        message = None
        if response.generations and response.generations[0]:
            message = getattr(response.generations[0][0], "message", None)

        usage = getattr(message, "usage_metadata", None) or {}
        meta = getattr(message, "response_metadata", None) or {}
        input_tokens = int(usage.get("input_tokens", 0))
        output_tokens = int(usage.get("output_tokens", 0))
        response_model = meta.get("model_name") or self._request_model
        finish_reason = meta.get("finish_reason")
        response_id = meta.get("id")

        run.span.set_attribute(RESPONSE_MODEL, response_model)
        run.span.set_attribute(INPUT_TOKENS, input_tokens)
        run.span.set_attribute(OUTPUT_TOKENS, output_tokens)
        if response_id:
            run.span.set_attribute(RESPONSE_ID, response_id)
        if finish_reason:
            run.span.set_attribute(FINISH_REASONS, [finish_reason])
            self.finish_reasons.append(finish_reason)

        self.total_input_tokens += input_tokens
        self.total_output_tokens += output_tokens

        attrs = self._metric_attrs(response_model)
        self._token_usage.record(input_tokens, {**attrs, TOKEN_TYPE: "input"})
        self._token_usage.record(output_tokens, {**attrs, TOKEN_TYPE: "output"})
        self._operation_duration.record(time.monotonic() - run.started, attrs)
        run.span.end()

    def on_llm_error(self, error: BaseException, *, run_id: UUID, **kwargs: Any) -> None:
        run = self._pop(run_id)
        if run is None:
            return
        # error.type is conditionally required when the operation errors.
        run.span.set_attribute(ERROR_TYPE, type(error).__name__)
        run.span.set_status(StatusCode.ERROR, str(error))
        attrs = self._metric_attrs(None)
        attrs[ERROR_TYPE] = type(error).__name__
        self._operation_duration.record(time.monotonic() - run.started, attrs)
        run.span.end()

    # ---------------------------------------------------------------- tools

    def on_tool_start(
        self,
        serialized: dict[str, Any],
        input_str: str,
        *,
        run_id: UUID,
        parent_run_id: UUID | None = None,
        **kwargs: Any,
    ) -> None:
        tool_name = (serialized or {}).get("name", "tool")
        span = self._tracer.start_span(
            f"execute_tool {tool_name}",
            context=self._parent_context(parent_run_id),
            kind=SpanKind.INTERNAL,  # the tool runs in-process
        )
        span.set_attribute(OPERATION_NAME, "execute_tool")
        span.set_attribute(TOOL_NAME, tool_name)  # required on execute_tool spans
        span.set_attribute(TOOL_TYPE, "function")
        # gen_ai.tool.call.id is the MODEL-assigned call id, correlating the
        # execution with the tool_call in the assistant message. LangChain
        # delivers it here as tool_call_id; the run UUID is a framework
        # internal and would be wrong. The attribute is only "recommended",
        # so omit it when the framework doesn't pass one.
        tool_call_id = kwargs.get("tool_call_id")
        if tool_call_id:
            span.set_attribute(TOOL_CALL_ID, str(tool_call_id))
        span.set_attribute(AGENT_NAME, self._agent_name)
        description = (serialized or {}).get("description")
        if description:
            span.set_attribute(TOOL_DESCRIPTION, description)
        self._track(run_id, span)

    def on_tool_end(self, output: Any, *, run_id: UUID, **kwargs: Any) -> None:
        run = self._pop(run_id)
        if run is not None:
            run.span.end()

    def on_tool_error(self, error: BaseException, *, run_id: UUID, **kwargs: Any) -> None:
        run = self._pop(run_id)
        if run is None:
            return
        run.span.set_attribute(ERROR_TYPE, type(error).__name__)
        run.span.set_status(StatusCode.ERROR, str(error))
        run.span.end()

    # ------------------------------------------------- retrieval graph node

    def on_chain_start(
        self,
        serialized: dict[str, Any],
        inputs: dict[str, Any],
        *,
        run_id: UUID,
        parent_run_id: UUID | None = None,
        **kwargs: Any,
    ) -> None:
        # LangGraph fires a chain event per graph node; only the retrieval
        # node becomes a span. Everything else (the graph itself, agent and
        # tool wrapper chains) stays invisible — the conventions describe
        # GenAI operations, not framework plumbing.
        name = kwargs.get("name") or (serialized or {}).get("name")
        if name != RETRIEVAL_NODE_NAME:
            return
        span = self._tracer.start_span(
            f"retrieval {self._data_source_id}",
            context=self._parent_context(parent_run_id),
            kind=SpanKind.CLIENT,  # retrieval spans are CLIENT per the conventions
        )
        span.set_attribute(OPERATION_NAME, "retrieval")
        span.set_attribute(DATA_SOURCE_ID, self._data_source_id)
        span.set_attribute(RETRIEVAL_TOP_K, self._retrieval_top_k)
        self._track(run_id, span)

    def on_chain_end(self, outputs: dict[str, Any], *, run_id: UUID, **kwargs: Any) -> None:
        run = self._pop(run_id)
        if run is not None:
            run.span.end()

    def on_chain_error(self, error: BaseException, *, run_id: UUID, **kwargs: Any) -> None:
        run = self._pop(run_id)
        if run is None:
            return
        run.span.set_attribute(ERROR_TYPE, type(error).__name__)
        run.span.set_status(StatusCode.ERROR, str(error))
        run.span.end()
