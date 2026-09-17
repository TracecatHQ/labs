"""Pure scoring helpers mirrored by the Tracecat scorer workflow."""


def incident_id_from_trial(trial: dict) -> str:
    return str(trial.get("submission", {}).get("case", {}).get("payload", {}).get("incident_id", ""))


def discounted_partial_reward(step_results: list[bool], discount: float = 0.4) -> float:
    """Match SecRL: reverse steps, skip the final-answer step, discount backward."""
    total = 0.0
    current = discount
    for correct in list(reversed(step_results))[1:]:
        if correct:
            total += current
        if total >= 1.0:
            return 1.0
        current *= discount
    return total


def score(answer_correct: bool, answer_reflection: bool, step_results: list[bool], step_reflection: list[bool]) -> tuple[int, float]:
    success = int(bool(answer_correct and answer_reflection))
    if success:
        return 1, 1.0
    reflected = [a and b for a, b in zip(step_results, step_reflection, strict=True)]
    return 0, discounted_partial_reward(reflected)
