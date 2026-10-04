"""The agent loop: plan → tools → retrieval → answer, fully instrumented.

The trace shape this produces is the one the conformance suite's topology
rules expect: an INTERNAL invoke_agent span parenting the CLIENT chat calls,
INTERNAL tool executions, and the CLIENT retrieval span.
"""

from dataclasses import dataclass

from agent_demo.llm import Provider
from agent_demo.retrieval import Hit, VectorStore
from agent_demo.telemetry import GenAIInstrumentation
from agent_demo.tools import TOOLS


@dataclass(frozen=True)
class AskResult:
    answer: str
    response_id: str
    trace_id: str
    span_id: str
    conversation_id: str | None
    used_tools: list[str]
    retrieved: list[Hit]


class TripPlannerAgent:
    def __init__(
        self, instr: GenAIInstrumentation, provider: Provider, store: VectorStore | None = None
    ) -> None:
        self._instr = instr
        self._provider = provider
        self._store = store if store is not None else VectorStore()

    def ask(self, question: str, conversation_id: str | None = None) -> AskResult:
        user_message = [{"role": "user", "parts": [{"type": "text", "content": question}]}]

        with self._instr.invoke_agent(conversation_id) as run:
            # 1) Planning call: the model decides which tools to use.
            with self._instr.chat(user_message) as call:
                call.response = self._provider.plan(question)
            plan = call.response
            run.add_usage(plan)

            # 2) Execute the planned tool calls. Keep an ordered per-call
            # record (not just a name-keyed dict) so that if the plan calls
            # the same tool twice, each tool_call_response part below carries
            # its own call's output rather than the last one's.
            tool_results: dict[str, str] = {}
            tool_outputs: list[tuple[str, str, str]] = []  # (call id, tool name, result)
            used: list[str] = []
            for tc in plan.tool_calls:
                tool = TOOLS[tc.name]
                with self._instr.execute_tool(tool, tc.id):
                    result = tool.fn(**tc.arguments)
                tool_results[tc.name] = result
                tool_outputs.append((tc.id, tc.name, result))
                used.append(tc.name)

            # 3) Ground the answer in the local corpus.
            with self._instr.retrieval_span(top_k=3):
                hits = self._store.search(question, top_k=3)

            # 4) Final answer call. Its recorded input mirrors what the
            # provider actually receives — question plus tool results plus
            # retrieved context — shaped per the pinned
            # gen-ai-input-messages.json schema (tool_call_response parts),
            # not just the original user turn.
            docs = [h.document.text for h in hits]
            answer_input = list(user_message)
            if tool_outputs:
                answer_input.append(
                    {
                        "role": "tool",
                        "parts": [
                            {"type": "tool_call_response", "id": call_id, "response": result}
                            for call_id, _name, result in tool_outputs
                        ],
                    }
                )
            if docs:
                answer_input.append(
                    {
                        "role": "user",
                        "parts": [{"type": "text", "content": "Context:\n" + "\n\n".join(docs)}],
                    }
                )
            with self._instr.chat(answer_input) as call:
                call.response = self._provider.answer(question, tool_results, docs)
            answer = call.response
            run.add_usage(answer)
            run.finish_reasons = [answer.finish_reason]
            run.response_id = answer.response_id

            return AskResult(
                answer=answer.text,
                response_id=answer.response_id,
                trace_id=run.trace_id_hex,
                span_id=run.span_id_hex,
                conversation_id=conversation_id,
                used_tools=used,
                retrieved=hits,
            )
