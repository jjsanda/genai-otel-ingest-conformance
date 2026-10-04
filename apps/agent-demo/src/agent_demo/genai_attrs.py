"""GenAI semantic-convention attribute names.

Pinned to open-telemetry/semantic-conventions-genai commit b028dce (see
third_party/semconv-genai/PINNED_SHA at the repo root). Kept as constants in
one module so a convention rename is a one-line diff, mirroring how the Go
side pins the same snapshot in its curated registry.
"""

GEN_AI_OPERATION_NAME = "gen_ai.operation.name"
GEN_AI_PROVIDER_NAME = "gen_ai.provider.name"
GEN_AI_REQUEST_MODEL = "gen_ai.request.model"
GEN_AI_REQUEST_TEMPERATURE = "gen_ai.request.temperature"
GEN_AI_REQUEST_MAX_TOKENS = "gen_ai.request.max_tokens"
GEN_AI_RESPONSE_ID = "gen_ai.response.id"
GEN_AI_RESPONSE_MODEL = "gen_ai.response.model"
GEN_AI_RESPONSE_FINISH_REASONS = "gen_ai.response.finish_reasons"
GEN_AI_USAGE_INPUT_TOKENS = "gen_ai.usage.input_tokens"
GEN_AI_USAGE_OUTPUT_TOKENS = "gen_ai.usage.output_tokens"
GEN_AI_TOKEN_TYPE = "gen_ai.token.type"
GEN_AI_CONVERSATION_ID = "gen_ai.conversation.id"
GEN_AI_AGENT_NAME = "gen_ai.agent.name"
GEN_AI_AGENT_DESCRIPTION = "gen_ai.agent.description"
GEN_AI_TOOL_NAME = "gen_ai.tool.name"
GEN_AI_TOOL_CALL_ID = "gen_ai.tool.call.id"
GEN_AI_TOOL_TYPE = "gen_ai.tool.type"
GEN_AI_TOOL_DESCRIPTION = "gen_ai.tool.description"
GEN_AI_DATA_SOURCE_ID = "gen_ai.data_source.id"
GEN_AI_RETRIEVAL_TOP_K = "gen_ai.retrieval.top_k"
GEN_AI_SYSTEM_INSTRUCTIONS = "gen_ai.system_instructions"
GEN_AI_INPUT_MESSAGES = "gen_ai.input.messages"
GEN_AI_OUTPUT_MESSAGES = "gen_ai.output.messages"
GEN_AI_EVALUATION_NAME = "gen_ai.evaluation.name"
GEN_AI_EVALUATION_SCORE_VALUE = "gen_ai.evaluation.score.value"
GEN_AI_EVALUATION_SCORE_LABEL = "gen_ai.evaluation.score.label"
GEN_AI_EVALUATION_EXPLANATION = "gen_ai.evaluation.explanation"
ERROR_TYPE = "error.type"
SERVER_ADDRESS = "server.address"
SERVER_PORT = "server.port"

# Event names.
EVENT_EVALUATION_RESULT = "gen_ai.evaluation.result"

# Operation name values used by this demo.
OP_CHAT = "chat"
OP_EXECUTE_TOOL = "execute_tool"
OP_INVOKE_AGENT = "invoke_agent"
OP_RETRIEVAL = "retrieval"

# --- Deprecated names, used ONLY by the intentionally broken emitter -------
DEPRECATED_GEN_AI_SYSTEM = "gen_ai.system"
DEPRECATED_PROMPT_TOKENS = "gen_ai.usage.prompt_tokens"
DEPRECATED_COMPLETION_TOKENS = "gen_ai.usage.completion_tokens"
LEGACY_EVENT_CONTENT_PROMPT = "gen_ai.content.prompt"
# Not a real convention at any version — a typo-style attribute the
# conformance suite flags as unknown.
BOGUS_TOTAL_TOKENS = "gen_ai.usage.total_tokens"
