"""The travel-helper LangGraph: retrieval → agent ⇄ tools → answer.

The graph is intentionally small — its job is to exercise the shapes the
GenAI conventions describe (an in-process agent invoking a model that calls
tools, with a retrieval step for grounding), not to be a clever agent.
"""

from __future__ import annotations

from typing import Annotated, TypedDict

from langchain_core.language_models import BaseChatModel
from langchain_core.messages import AnyMessage, HumanMessage, SystemMessage
from langchain_core.runnables import RunnableConfig
from langchain_core.tools import tool
from langgraph.graph import END, StateGraph
from langgraph.graph.message import add_messages
from langgraph.prebuilt import ToolNode

# A deliberately tiny keyword-scored corpus: demo B's retrieval exists to
# produce a spec-shaped `retrieval {data source}` span, not to showcase
# vector search (demo A has the embedding store).
CORPUS: dict[str, str] = {
    "doc-vienna": "Vienna's historic centre is a UNESCO site; the U-Bahn runs all night weekends.",
    "doc-prague": "Prague's Charles Bridge dates to 1357 and is pedestrian-only.",
    "doc-paris": "Paris has more than 130 museums; the Métro's line 1 is fully automated.",
    "doc-tokyo": "Tokyo's Yamanote loop line connects most major city centres in about an hour.",
    "doc-lisbon": "Lisbon's tram 28 climbs the Alfama hills; ticket machines take cards only.",
    "doc-oslo": "Oslo's ferries to the fjord islands are covered by the regular transit pass.",
    "doc-kyoto": "Kyoto has over 1,600 Buddhist temples; buses use a flat fare in the city core.",
    "doc-porto": "Porto's Dom Luís I bridge has a walkable upper deck with river views.",
}

DATA_SOURCE_ID = "lg-city-kb"
RETRIEVAL_TOP_K = 3
AGENT_NAME = "langgraph-travel-helper"


@tool
def unit_converter(value: float, unit: str) -> str:
    """Convert between km and miles, or between °C and °F."""
    unit = unit.lower().strip()
    if unit in ("km", "kilometers"):
        return f"{value} km is {value * 0.621371:.1f} miles"
    if unit in ("miles", "mi"):
        return f"{value} miles is {value * 1.609344:.1f} km"
    if unit in ("°c", "c", "celsius"):
        return f"{value}°C is {value * 9 / 5 + 32:.1f}°F"
    if unit in ("°f", "f", "fahrenheit"):
        return f"{value}°F is {(value - 32) * 5 / 9:.1f}°C"
    return f"unsupported unit {unit!r}"


@tool
def city_facts(city: str) -> str:
    """Look up a quick fact about a city."""
    key = f"doc-{city.lower()}"
    return CORPUS.get(key, f"No stored fact about {city}.")


TOOLS = [unit_converter, city_facts]


class AgentState(TypedDict):
    question: str
    docs: list[str]
    messages: Annotated[list[AnyMessage], add_messages]


def _score(question: str, text: str) -> int:
    words = {w.strip(".,?!").lower() for w in question.split()}
    return sum(1 for w in text.lower().split() if w.strip(".,;") in words)


def build_graph(model: BaseChatModel):
    """Compile the graph around the given chat model."""
    model_with_tools = model.bind_tools(TOOLS)

    def retrieval(state: AgentState) -> dict:
        # The telemetry bridge turns this node's chain-start/-end callbacks
        # into the `retrieval {data source}` span; the node itself stays
        # telemetry-free.
        ranked = sorted(CORPUS.values(), key=lambda text: -_score(state["question"], text))
        return {"docs": ranked[:RETRIEVAL_TOP_K]}

    def agent(state: AgentState, config: RunnableConfig) -> dict:
        # Passing config through is what propagates the callback handler to
        # the model call — without it the bridge would never hear about it.
        if state["messages"]:
            response = model_with_tools.invoke(state["messages"], config=config)
            return {"messages": [response]}
        seed: list[AnyMessage] = [
            SystemMessage(
                content="You are a travel helper. Ground answers in this context: "
                + " ".join(state["docs"])
            ),
            HumanMessage(content=state["question"]),
        ]
        response = model_with_tools.invoke(seed, config=config)
        return {"messages": [*seed, response]}

    def should_continue(state: AgentState) -> str:
        last = state["messages"][-1]
        return "tools" if getattr(last, "tool_calls", None) else END

    graph = StateGraph(AgentState)
    graph.add_node("retrieval", retrieval)
    graph.add_node("agent", agent)
    graph.add_node("tools", ToolNode(TOOLS))
    graph.set_entry_point("retrieval")
    graph.add_edge("retrieval", "agent")
    graph.add_conditional_edges("agent", should_continue, {"tools": "tools", END: END})
    graph.add_edge("tools", "agent")
    return graph.compile()
