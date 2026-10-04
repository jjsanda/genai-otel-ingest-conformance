"""The agent's tools: a safe calculator and a static city-facts lookup.

The calculator walks a parsed AST instead of using eval() — the demo's
telemetry may be pointed at by strangers on the internet, so it should model
good habits even where it does not matter.
"""

import ast
import json
import operator
from collections.abc import Callable
from dataclasses import dataclass


@dataclass(frozen=True)
class Tool:
    name: str
    description: str
    fn: Callable[..., str]


_BIN_OPS: dict[type[ast.operator], Callable[[float, float], float]] = {
    ast.Add: operator.add,
    ast.Sub: operator.sub,
    ast.Mult: operator.mul,
    ast.Div: operator.truediv,
    ast.Pow: operator.pow,
    ast.Mod: operator.mod,
}


def _eval_node(node: ast.expr) -> float:
    match node:
        case ast.Constant(value=v) if isinstance(v, int | float):
            return float(v)
        case ast.BinOp(left=left, op=op, right=right) if type(op) in _BIN_OPS:
            return _BIN_OPS[type(op)](_eval_node(left), _eval_node(right))
        case ast.UnaryOp(op=ast.USub(), operand=operand):
            return -_eval_node(operand)
        case ast.UnaryOp(op=ast.UAdd(), operand=operand):
            return _eval_node(operand)
        case _:
            raise ValueError(f"unsupported expression element: {ast.dump(node)}")


def calculator(expression: str) -> str:
    """Evaluate a basic arithmetic expression (+ - * / ** % and parentheses)."""
    try:
        tree = ast.parse(expression.strip(), mode="eval")
        value = _eval_node(tree.body)
    except (SyntaxError, ValueError, ZeroDivisionError) as exc:
        return f"calculator error: {exc}"
    # Render integers without a trailing .0 so answers read naturally.
    return str(int(value)) if value == int(value) else f"{value:.6g}"


CITY_FACTS: dict[str, dict[str, str]] = {
    "paris": {"country": "France", "population": "2.1 million", "currency": "EUR"},
    "rome": {"country": "Italy", "population": "2.8 million", "currency": "EUR"},
    "tokyo": {"country": "Japan", "population": "14.0 million", "currency": "JPY"},
    "prague": {"country": "Czechia", "population": "1.3 million", "currency": "CZK"},
    "barcelona": {"country": "Spain", "population": "1.6 million", "currency": "EUR"},
    "london": {"country": "United Kingdom", "population": "8.9 million", "currency": "GBP"},
    "vienna": {"country": "Austria", "population": "2.0 million", "currency": "EUR"},
}


def city_facts(city: str) -> str:
    """Look up structured facts about a city."""
    facts = CITY_FACTS.get(city.strip().lower())
    if facts is None:
        return json.dumps({"city": city, "error": "no facts on record"})
    return json.dumps({"city": city.title(), **facts})


TOOLS: dict[str, Tool] = {
    "calculator": Tool(
        name="calculator",
        description="Evaluate a basic arithmetic expression",
        fn=calculator,
    ),
    "city_facts": Tool(
        name="city_facts",
        description="Look up country, population, and currency for a city",
        fn=city_facts,
    ),
}
