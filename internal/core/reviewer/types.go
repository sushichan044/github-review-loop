package reviewer

import "time"

// Type identifies the kind of reviewer actor.
type Type string

const (
	ReviewerTypeUser          Type = "user"
	ReviewerTypeGitHubCopilot Type = "github-copilot"
	ReviewerTypeGitHubApp     Type = "github-app"
)

// Identity uniquely identifies a reviewer. Name comparison is case-insensitive.
// For github-copilot, Name may be empty.
type Identity struct {
	Type Type
	Name string
}

// Goal defines the success condition for a reviewer policy.
type Goal string

const (
	// GoalApproved is met when the reviewer's latest non-pending review on the
	// current head is Approved.
	GoalApproved Goal = "approved"

	// GoalReviewedClean is met when the reviewer's latest non-pending review on the
	// current head signs off with no outstanding inline findings: either Approved,
	// or Commented with zero inline comments. Resolving threads is not sufficient —
	// the reviewer must re-review the latest commit cleanly.
	GoalReviewedClean Goal = "reviewed-clean"
)

// Policy describes the loop behavior for one reviewer.
type Policy struct {
	Identity   Identity
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
	Reviewer Identity
	At       time.Time
}

// Review represents a single review submission.
type Review struct {
	Reviewer  Identity
	State     ReviewState
	CommitOID string
	At        time.Time

	// Body is the review-level comment body (pullrequestreview.body), which can
	// carry findings not attached to any inline thread (e.g. CodeRabbit's
	// "outside diff range" comments). Empty when the reviewer left no body.
	Body string

	// ID is the review's GitHub databaseId, used to build a drill-in command
	// that reads this review's body.
	ID string

	// InlineCommentCount is the number of inline review comments attached to this
	// review submission. Zero means the reviewer left no inline findings — a
	// "clean" review.
	InlineCommentCount int
}

// Thread represents a review-conversation thread attributed to a reviewer.
// Issue comments are not threads.
type Thread struct {
	Reviewer Identity
	Resolved bool
}

// Snapshot is the abstract, VCS-agnostic input to the evaluator.
type Snapshot struct {
	HeadCommitOID string
	Triggers      []TriggerAction
	Reviews       []Review
	Threads       []Thread
}

// State is the result of evaluating one policy against a snapshot.
type State struct {
	// Identity is copied from the policy so callers can map results back to reviewers.
	Identity     Identity
	Phase        Phase
	RallyCount   int
	GoalMet      bool
	CanRerequest bool
	BlockReason  string

	// ChangesRequested is true when the reviewer's latest non-pending review
	// state is changes-requested. For a configured reviewer this gates the loop
	// (the reviewer stays non-terminal) and is sticky until they re-review with a
	// different state, matching GitHub's reviewDecision behavior.
	ChangesRequested bool

	// LatestReview* summarize the reviewer's most recent non-pending review for
	// rendering (changes-requested handling and the review-body drill-in). They
	// are zero when the reviewer has submitted no non-pending review.
	LatestReviewState       ReviewState
	LatestReviewCommitOID   string
	LatestReviewID          string
	LatestReviewBodyPresent bool
}

// LoopState is the aggregate result of evaluating all policies.
type LoopState struct {
	Reviewers []State
	Done      bool
}
