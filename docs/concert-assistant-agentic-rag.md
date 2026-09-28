# Concert Assistant Agentic RAG Roadmap

Concert Assistant currently uses a constrained LLM workflow for read-only
concert questions. This document describes the incremental path to a bounded
agentic RAG system without weakening its existing database and SQL safeguards.

## Current capabilities

| Capability | Status | Notes |
| --- | --- | --- |
| LLM query planning and routing | Implemented | Routes a question to analytical SQL, artist similarity, or an unsupported response. |
| Read-only SQL retrieval | Implemented | Generated SQL is validated before execution and may query only `public.concerts`. |
| Least-privileged data access | Implemented | Runtime uses the `concert_assistant_reader` PostgreSQL role. |
| Artist touring-profile vector retrieval | Implemented | pgvector finds similar artists from deterministic touring-history profiles. It does not infer genre or style. |
| Answer synthesis | Implemented | The LLM summarizes only SQL rows or retrieved artist-profile matches. |
| Safe unsupported-query response | Implemented | Requests requiring unavailable descriptive data are rejected instead of guessed. |
| Descriptive concert corpus | Not implemented | The source dataset has no venue, festival, review, or concert-description text suitable for broad semantic retrieval. |
| SQL + RAG hybrid answers | Not implemented | Requires a descriptive corpus and retrieval path. |
| Iterative, autonomous retrieval | Not implemented | The current router chooses one fixed path and does not select follow-up actions. |
| Retrieval evidence and citations | Not implemented | Answers do not yet expose the source records used as narrative evidence. |
| Retrieval evaluation suite | Not implemented | There is no labeled benchmark for MRR, nDCG, or Recall@K. |

## Target workflow

An agentic RAG request should run through a bounded loop:

```text
Question
  -> LLM creates a structured plan
  -> execute one constrained retrieval tool
  -> evaluate whether the retrieved evidence is sufficient
  -> optionally refine the retrieval plan and execute another tool
  -> synthesize an answer with evidence
```

The loop must have a maximum number of tool calls and an overall time budget.
If the available evidence remains insufficient, the assistant should say so
rather than infer unsupported facts.

## Implementation steps

1. **Add descriptive source data — Not implemented**

   Define the permitted source material for concert, venue, festival, or other
   descriptive context. Preserve source identity and provenance for every
   record.

2. **Build a document retrieval projection — Not implemented**

   Normalize and chunk descriptive records, create embeddings, store them in
   pgvector, and retain source IDs, text, timestamps, and provenance alongside
   each chunk. Refresh the projection deterministically after source updates.

3. **Expose constrained retrieval tools — Partially implemented**

   The existing validated SQL path and artist-similarity lookup are the first
   tools. Add a read-only descriptive-context retrieval tool with structured
   inputs and structured source-bearing outputs. The model must never receive
   direct PostgreSQL access.

4. **Expand planning into tool selection — Not implemented**

   Replace the one-shot route with a structured action plan. The planner can
   select SQL, artist similarity, descriptive retrieval, or a hybrid sequence,
   using only approved tool schemas.

5. **Add bounded reflection and refinement — Not implemented**

   After each tool result, evaluate evidence coverage, relevance, and whether
   the question is answerable. Permit query reformulation, filter adjustment,
   or a different approved retrieval tool, subject to call and time limits.

6. **Synthesize evidence-grounded answers — Partially implemented**

   Current synthesis is grounded in SQL rows or similarity matches. Extend it
   to distinguish structured facts from retrieved narrative context and return
   citations or source references for the latter.

7. **Create offline retrieval evaluations — Not implemented**

   Build a versioned set of representative questions with relevance judgments.
   Measure MRR, nDCG, and Recall@K for retrieval changes offline. These metrics
   require labeled relevance data and should not be treated as automatic live
   proof that a single answer is correct.

8. **Add agent observability and safety tests — Not implemented**

   Record the selected actions, retrieval summaries, retry count, stop reason,
   latency, and failures without logging sensitive prompt data. Test tool-call
   limits, unsupported requests, invalid SQL, insufficient-evidence responses,
   citation correctness, and deterministic evaluation regressions.

## Non-negotiable guardrails

- SQL validation and the read-only database role remain mandatory.
- The agent can invoke only application-defined, read-only tools.
- Tool outputs are structured and source-bearing; the model does not inspect
  database metadata or execute arbitrary SQL.
- Retrieval attempts are finite and time-bounded.
- The assistant must return an insufficient-evidence response when sources do
  not support an answer.
