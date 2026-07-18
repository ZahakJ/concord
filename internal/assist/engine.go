package assist

// Engine labels: which model actually answered.
//
// Concord's assistant can be served by more than one thing, and the user is
// entitled to know which one it was every single time. A summary written by a
// 3B model on this box and a summary written by a Claude session are different
// products with different privacy properties, and conflating them would be a
// lie of exactly the kind this app exists to avoid.
//
// So every assistant output carries an Engine and a human-readable note, and
// both are plumbed to the UI. A local answer never claims to be a brain
// answer — that invariant is worth more than the feature.

import "strings"

// Engine names the thing that produced an assistant answer.
type Engine string

const (
	// EngineBrain: a Claude Code session on this machine, on the user's own
	// subscription, answered via Aether's queue. Local process — but Claude
	// saw the job's content.
	EngineBrain Engine = "brain"
	// EngineLocal: an Ollama model on this machine over loopback. Nothing
	// left the box.
	EngineLocal Engine = "local"
	// EngineAPI is defined for symmetry with the rest of the portfolio and is
	// deliberately never produced by Concord. See the note on APITierAbsent.
	EngineAPI Engine = "api"
	// EngineNone: nothing answered. Used with an honest error.
	EngineNone Engine = ""
)

// APITierAbsent documents a deliberate gap in the routing seam.
//
// The portfolio-wide seam is: brain -> local specialist -> API key (only if
// configured) -> honest failure. Concord configures no API key and never will.
// A remote API call would break the structural guarantee in
// ValidateEndpoint — that there is no configuration in which assistant traffic
// leaves this machine — and that guarantee is the product. So Concord's seam
// is brain -> local specialist -> honest failure, with the API tier removed on
// purpose rather than left unimplemented.
const APITierAbsent = "concord has no API tier by design: loopback or nothing"

// Local specialists this machine may have pulled. When present they beat the
// user's configured general model at their one job; when absent everything
// falls back to the configured model and nothing breaks.
const (
	// SpecialistBrief turns an event ledger into a short plain-language
	// recap — exactly the "catch me up" shape. Contract:
	// ~/work/apex_llm/INTEGRATION.md, "aether-brief".
	SpecialistBrief = "aether-brief"
)

// Result is one assistant answer plus the provenance the UI must show.
type Result struct {
	Text   string `json:"text"`
	Engine Engine `json:"engine"`
	// Note is the one-line, human-readable provenance shown in the UI.
	Note string `json:"note"`
	// Model is the local model actually used, when Engine is local.
	Model string `json:"model,omitempty"`
	// JobID is set when a brain job was accepted; poll it for the answer.
	JobID string `json:"jobId,omitempty"`
	// Pending means a brain job is queued and has no answer yet. Not a
	// failure — the brain answers when a session picks the job up.
	Pending bool `json:"pending"`
}

// LocalResult labels an answer that came from the on-device model.
func LocalResult(text, model string) Result {
	return Result{Text: text, Engine: EngineLocal, Model: model, Note: LocalNote(model)}
}

// BrainResult labels an answer that came from the shared brain.
func BrainResult(text, jobID string) Result {
	return Result{Text: text, Engine: EngineBrain, JobID: jobID, Note: BrainNote()}
}

// PendingResult labels an accepted-but-unanswered brain job.
//
// Deliberately does NOT guess at why it is pending. The job may be queued
// behind others, or a session may be working on it right now — Concord cannot
// tell the two apart from a job id, and a note that asserts either would
// sometimes be wrong.
func PendingResult(jobID string) Result {
	return Result{
		Engine: EngineBrain, JobID: jobID, Pending: true,
		Note: "Queued for the shared brain — a Claude Code session on this " +
			"machine answers these as it gets to them. Nothing has been sent " +
			"anywhere else, and you can close this and use the local model instead.",
	}
}

// LocalNote describes the on-device path in one line.
func LocalNote(model string) string {
	m := strings.TrimSpace(model)
	if m == "" {
		m = "your local model"
	}
	if m == SpecialistBrief {
		return "Written on this device by the " + m + " specialist model — " +
			"the conversation never left this machine."
	}
	return "Written on this device by " + m + " over localhost — the " +
		"conversation never left this machine."
}

// BrainNote describes the brain path in one line, honestly.
//
// This copy is load-bearing. The brain is local in the sense that matters for
// billing and for network egress to third parties, and NOT local in the sense
// the rest of this app means it: Claude read the message text. Say both.
func BrainNote() string {
	return "Written by the shared brain: a Claude Code session running on this " +
		"machine on your own subscription. The process is local and no API key " +
		"is used — but Claude did see the messages in this request."
}
