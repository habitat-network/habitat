"""Scores the samples a ragbench run wrote (samples.jsonl) with Ragas.

ragbench (the Go command in ../..) asks pear's MCP server each question of the
benchmark and records what came back. This reads those samples and scores:

- which answering documents `get_context` returned, by document id
  (IDBasedContextRecall, IDBasedContextPrecision),
- whether it suggested the tool that searches where the answer is (ToolCallF1,
  treating the suggestions as the calls an agent following them would make),
- with --llm, whether the text it returned is enough to answer the question
  (ContextRecall, judged by a model against the reference answer), and, for
  samples carrying an agent's `response`, whether that answer is right
  (FactualCorrectness) and grounded in what was retrieved (Faithfulness).

The first two need no model and give the same scores on every run.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import statistics
import sys
import warnings
from collections import defaultdict
from collections.abc import Awaitable, Callable, Iterable
from pathlib import Path
from typing import Any

Sample = dict[str, Any]
Scores = dict[str, float]

# How many LLM-judged samples are in flight at once.
LLM_CONCURRENCY = 8


def read_samples(path: Path) -> list[Sample]:
    """Reads samples.jsonl, leaving out questions get_context failed on."""
    samples = []
    with path.open() as f:
        for line in f:
            if line.strip():
                sample = json.loads(line)
                if not sample.get("error"):
                    samples.append(sample)
    return samples


def tool_calls(names: Iterable[str]) -> list[Any]:
    from ragas.messages import ToolCall

    return [ToolCall(name=name, args={}) for name in names]


async def score_deterministic(sample: Sample, top_tools: int) -> Scores:
    """Scores a sample with the metrics that need no model."""
    from ragas.dataset_schema import SingleTurnSample
    from ragas.messages import AIMessage, HumanMessage
    from ragas.metrics import IDBasedContextPrecision, IDBasedContextRecall
    from ragas.metrics.collections import ToolCallF1

    by_id = SingleTurnSample(
        retrieved_context_ids=sample["retrieved_context_ids"],
        reference_context_ids=sample["reference_context_ids"],
    )
    scores = {
        "id_context_recall": await IDBasedContextRecall().single_turn_ascore(by_id),
        "id_context_precision": await IDBasedContextPrecision().single_turn_ascore(by_id),
    }
    reference_tools = sample.get("reference_tools") or []
    if reference_tools:
        suggested = [tool["name"] for tool in sample["suggested_tools"][:top_tools]]
        result = await ToolCallF1().ascore(
            user_input=[
                HumanMessage(content=sample["user_input"]),
                AIMessage(content="", tool_calls=tool_calls(suggested)),
            ],
            reference_tool_calls=tool_calls(reference_tools),
        )
        scores[f"tool_f1_top{top_tools}"] = float(result.value)
    return {name: float(value) for name, value in scores.items()}


def judge(provider: str, model: str) -> Any:
    """Returns the Ragas LLM that judges the --llm metrics."""
    from ragas.llms import llm_factory

    if provider == "anthropic":
        from anthropic import AsyncAnthropic

        return llm_factory(model, provider="anthropic", client=AsyncAnthropic())
    if provider == "openai":
        from openai import AsyncOpenAI

        return llm_factory(model, provider="openai", client=AsyncOpenAI())
    raise ValueError(f"unknown provider {provider!r}: use anthropic or openai")


async def score_with_llm(sample: Sample, llm: Any) -> Scores:
    """Scores a sample with the metrics a model judges."""
    from ragas.metrics.collections import ContextRecall, FactualCorrectness, Faithfulness

    scores: Scores = {}
    contexts = sample.get("retrieved_contexts") or []
    if contexts and sample.get("reference"):
        result = await ContextRecall(llm=llm).ascore(
            user_input=sample["user_input"],
            retrieved_contexts=contexts,
            reference=sample["reference"],
        )
        scores["context_recall"] = float(result.value)
    response = sample.get("response")
    if response and sample.get("reference"):
        result = await FactualCorrectness(llm=llm).ascore(
            response=response, reference=sample["reference"]
        )
        scores["factual_correctness"] = float(result.value)
    if response and contexts:
        result = await Faithfulness(llm=llm).ascore(
            user_input=sample["user_input"], response=response, retrieved_contexts=contexts
        )
        scores["faithfulness"] = float(result.value)
    return scores


async def score_all(
    samples: list[Sample],
    top_tools: int,
    llm_scorer: Callable[[Sample], Awaitable[Scores]] | None = None,
) -> list[Scores]:
    """Scores every sample; llm_scorer adds the model-judged metrics."""
    limit = asyncio.Semaphore(LLM_CONCURRENCY)

    async def one(sample: Sample) -> Scores:
        scores = await score_deterministic(sample, top_tools)
        if llm_scorer is not None:
            async with limit:
                scores.update(await llm_scorer(sample))
        return scores

    return list(await asyncio.gather(*(one(sample) for sample in samples)))


def groups(sample: Sample) -> list[str]:
    """The report rows a sample counts toward."""
    return [
        "all",
        f"type: {sample['question_type']}",
        f"source: {'+'.join(sample['source_types'])}",
    ]


def summarize(samples: list[Sample], scores: list[Scores]) -> str:
    """Renders the mean of each metric per group as a Markdown table."""
    metrics: list[str] = []
    by_group: dict[str, list[Scores]] = defaultdict(list)
    for sample, sample_scores in zip(samples, scores, strict=True):
        for metric in sample_scores:
            if metric not in metrics:
                metrics.append(metric)
        for group in groups(sample):
            by_group[group].append(sample_scores)

    def order(group: str) -> tuple[int, int, str]:
        kind = 0 if group == "all" else 1 if group.startswith("type") else 2
        return (kind, -len(by_group[group]), group)

    lines = [
        "| | questions | " + " | ".join(metrics) + " |",
        "|---|---|" + "---|" * len(metrics),
    ]
    for group in sorted(by_group, key=order):
        cells = []
        for metric in metrics:
            values = [s[metric] for s in by_group[group] if metric in s]
            cells.append(f"{statistics.fmean(values):.2f}" if values else "")
        lines.append(f"| {group} | {len(by_group[group])} | " + " | ".join(cells) + " |")
    return "\n".join(lines) + "\n"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("samples", type=Path, help="samples.jsonl written by ragbench")
    parser.add_argument(
        "--top-tools",
        type=int,
        default=3,
        help="how many of get_context's suggestions count as followed (default 3)",
    )
    parser.add_argument(
        "--llm",
        action="store_true",
        help="also run the metrics a model judges; needs the provider's API key",
    )
    parser.add_argument(
        "--provider",
        default=os.environ.get("RAGBENCH_JUDGE_PROVIDER", "anthropic"),
        help="judge's provider: anthropic or openai (or $RAGBENCH_JUDGE_PROVIDER)",
    )
    parser.add_argument(
        "--model",
        default=os.environ.get("RAGBENCH_JUDGE_MODEL", "claude-haiku-5-5"),
        help="judge's model (or $RAGBENCH_JUDGE_MODEL)",
    )
    args = parser.parse_args(argv)

    # Ragas warns about its own deprecated imports on every run.
    warnings.filterwarnings("ignore", category=DeprecationWarning)
    samples = read_samples(args.samples)
    if not samples:
        print(f"no scorable samples in {args.samples}", file=sys.stderr)
        return 1
    llm_scorer = None
    if args.llm:
        llm = judge(args.provider, args.model)

        async def llm_scorer(sample: Sample) -> Scores:
            return await score_with_llm(sample, llm)

    scores = asyncio.run(score_all(samples, args.top_tools, llm_scorer))

    out_dir = args.samples.parent
    with (out_dir / "ragas-scores.jsonl").open("w") as f:
        for sample, sample_scores in zip(samples, scores, strict=True):
            f.write(json.dumps({"question_id": sample["question_id"], **sample_scores}) + "\n")
    report = summarize(samples, scores)
    (out_dir / "ragas-summary.md").write_text(report)
    print(report)
    print(f"Per-question scores in {out_dir / 'ragas-scores.jsonl'}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
