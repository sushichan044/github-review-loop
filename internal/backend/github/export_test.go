package github

import "time"

// InjectTimeline exposes injectTimeline to the external github_test package.
func InjectTimeline(
	q any,
	headOID string,
	reviews []FakeReview,
	reqEvents []FakeReviewRequest,
	comments []FakeIssueComment,
) {
	injectTimeline(q, headOID, reviews, reqEvents, comments)
}

// InjectThreads exposes injectThreads to the external github_test package.
func InjectThreads(q any, threads []FakeThread) {
	injectThreads(q, threads)
}

// InjectPRMergeResult exposes injectPRMergeResult to the external github_test package.
func InjectPRMergeResult(q any, r FakePRMergeResult) {
	injectPRMergeResult(q, r)
}

// Exported type aliases so the external github_test package can reference
// the fake data types defined in testutil_test.go.
type (
	ExportedFakeReview        = FakeReview
	ExportedFakeReviewRequest = FakeReviewRequest
	ExportedFakeIssueComment  = FakeIssueComment
	ExportedFakeThread        = FakeThread
	ExportedFakePRMergeResult = FakePRMergeResult
	ExportedFakeCheck         = FakeCheck
)

// NewBackend exposes newBackend to the external github_test package.
func NewBackend(client *Client, opts ...backendOption) *GitHubBackend {
	return newBackend(client, opts...)
}

// BackendOption is the exported type alias for backendOption.
type BackendOption = backendOption

// WithRetrySleeper exposes withRetrySleeper to the external github_test package.
func WithRetrySleeper(fn func(time.Duration)) backendOption {
	return withRetrySleeper(fn)
}

// WithRetryCount exposes withRetryCount to the external github_test package.
func WithRetryCount(n int) backendOption {
	return withRetryCount(n)
}
