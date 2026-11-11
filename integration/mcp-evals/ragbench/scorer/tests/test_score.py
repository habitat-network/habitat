import asyncio
import json

from ragbench_scorer import score


def sample(**overrides):
    base = {
        "question_id": "qst_0001",
        "question_type": "basic",
        "source_types": ["github"],
        "user_input": "What is the default upload limit?",
        "reference": "10 MiB per file.",
        "reference_context_ids": ["dsid_a", "dsid_b"],
        "retrieved_context_ids": ["dsid_x", "dsid_a"],
        "retrieved_contexts": ["PR 1\nRaises the upload limit to 10 MiB."],
        "suggested_tools": [
            {"name": "github:search_pull_requests", "score": 0.4},
            {"name": "slack:slack_search_public", "score": 0.2},
        ],
        "reference_tools": ["github:search_pull_requests"],
    }
    return base | overrides


def test_score_deterministic_scores_ids_and_tools():
    scores = asyncio.run(score.score_deterministic(sample(), top_tools=2))

    # One of the two answering documents came back, among two results.
    assert scores["id_context_recall"] == 0.5
    assert scores["id_context_precision"] == 0.5
    # The right tool and one wrong one: precision 1/2, recall 1.
    assert round(scores["tool_f1_top2"], 2) == 0.67


def test_score_deterministic_only_counts_followed_suggestions():
    scores = asyncio.run(score.score_deterministic(sample(), top_tools=1))

    assert scores["tool_f1_top1"] == 1.0


def test_score_all_adds_llm_scores():
    async def fake_llm(s):
        return {"context_recall": 1.0}

    scores = asyncio.run(score.score_all([sample()], 3, fake_llm))

    assert scores[0]["context_recall"] == 1.0
    assert "id_context_recall" in scores[0]


def test_read_samples_skips_failed_questions(tmp_path):
    path = tmp_path / "samples.jsonl"
    path.write_text(
        json.dumps(sample()) + "\n" + json.dumps(sample(error="get_context failed")) + "\n"
    )

    assert len(score.read_samples(path)) == 1


def test_main_writes_scores_and_summary(tmp_path, capsys):
    path = tmp_path / "samples.jsonl"
    path.write_text(
        json.dumps(sample())
        + "\n"
        + json.dumps(sample(question_id="qst_0002", source_types=["slack"]))
        + "\n"
    )

    assert score.main([str(path)]) == 0

    rows = [json.loads(line) for line in (tmp_path / "ragas-scores.jsonl").read_text().splitlines()]
    assert [row["question_id"] for row in rows] == ["qst_0001", "qst_0002"]
    summary = (tmp_path / "ragas-summary.md").read_text()
    assert "| all | 2 |" in summary
    assert "| source: slack | 1 |" in summary
    assert summary in capsys.readouterr().out
