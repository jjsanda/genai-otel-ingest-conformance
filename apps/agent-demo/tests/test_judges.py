from agent_demo.evals.judges import judge_correctness, judge_groundedness, judge_tool_precision


def test_correctness_pass_and_fail():
    assert (
        judge_correctness("Planning notes for Paris: ...", "Planning notes for Paris").score == 1.0
    )
    j = judge_correctness("something else entirely", "Planning notes for Paris")
    assert j.score == 0.0 and j.label == "fail"


def test_groundedness_rewards_quoting_sources():
    doc = "Paris is the capital of France, famous for the Eiffel Tower and the Louvre museum."
    grounded = judge_groundedness(f"From the notes: {doc}", [doc])
    assert grounded.label == "grounded" and grounded.score > 0.5

    hallucinated = judge_groundedness(
        "Definitely visit the underwater volcano observatory on the moon colony.", [doc]
    )
    assert hallucinated.label == "ungrounded" and hallucinated.score < 0.35


def test_groundedness_empty_answer():
    assert judge_groundedness("", ["some doc"]).score == 0.0


def test_tool_precision_cases():
    assert judge_tool_precision([], []).score == 1.0  # nothing expected, nothing used
    assert judge_tool_precision([], ["calculator"]).score == 0.0
    assert judge_tool_precision(["calculator"], ["calculator"]).score == 1.0
    half = judge_tool_precision(["calculator", "city_facts"], ["calculator"])
    assert half.score == 0.5 and half.label == "incorrect"
