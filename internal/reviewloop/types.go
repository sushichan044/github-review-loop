package reviewloop

import "time"

// ReviewerType identifies the kind of reviewer actor.
type ReviewerType string

const (
	ReviewerTypeUser          ReviewerType = "user"
	ReviewerTypeGitHubCopilot ReviewerType = "github-copilot"
	ReviewerTypeGitHubApp     ReviewerType = "github-app"
)

// ReviewerIdentity uniquely identifies a reviewer. Name comparison is case-insensitive.
// For github-copilot, Name may be empty.
type ReviewerIdentity struct {
	Type ReviewerType
	Name string
}

// Goal defines the success condition for a reviewer policy.
type Goal string

const (
	// GoalApproved is met when the reviewer's latest non-pending review is Approved.
	GoalApproved Goal = "approved"

	// GoalAllConversationsResolved is met when the reviewer has no unresolved threads.
	GoalAllConversationsResolved Goal = "all-conversations-resolved"
)

// Policy describes the loop behavior for one reviewer.
type Policy struct {
	Identity   ReviewerIdentity
	Goal       Goal
	MaxRallies int
	Trigger    string
}

// Phase is the current state of a reviewer in the loop.
type Phase string

const (
	// PhaseActive means the loop is still running for this reviewer.
	PhaseActive Phase = "active"

	// PhaseGoalMet is a terminal phase: the reviewer's goal has been satisfied.
	PhaseGoalMet Phase = "goal-met"

	// PhaseExhausted is a terminal phase: RallyCount has reached MaxRallies without goal completion.
	PhaseExhausted Phase = "exhausted"
)

// ReviewState represents the state of a submitted review.
type ReviewState string

const (
	ReviewStateApproved         ReviewState = "approved"
	ReviewStateChangesRequested ReviewState = "changes-requested"
	ReviewStateCommented        ReviewState = "commented"
	ReviewStateDismissed        ReviewState = "dismissed"
	ReviewStatePending          ReviewState = "pending"
)

// TriggerAction records one re-request action targeting a reviewer.
type TriggerAction struct {
	Reviewer ReviewerIdentity
	At       time.Time
}

// Review represents a single review submission.
type Review struct {
	Reviewer  ReviewerIdentity
	State     ReviewState
	CommitOID string
	At        time.Time
}

// Thread represents a review-conversation thread attributed to a reviewer.
// Issue comments are not threads.
type Thread struct {
	Reviewer ReviewerIdentity
	Resolved bool
}

// Snapshot is the abstract, VCS-agnostic input to the evaluator.
type Snapshot struct {
	HeadCommitOID string
	Triggers      []TriggerAction
	Reviews       []Review
	Threads       []Thread
}

// ReviewerState is the result of evaluating one policy against a snapshot.
type ReviewerState struct {
	// Identity is copied from the policy so callers can map results back to reviewers.
	Identity     ReviewerIdentity
	Phase        Phase
	RallyCount   int
	GoalMet      bool
	CanRerequest bool
	BlockReason  string
}

// LoopState is the aggregate result of evaluating all policies.
type LoopState struct {
	Reviewers []ReviewerState
	Done      bool
}
