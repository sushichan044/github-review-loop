package github

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

// Exported type aliases so the external github_test package can reference
// the fake data types defined in testutil_test.go.
type (
	ExportedFakeReview        = FakeReview
	ExportedFakeReviewRequest = FakeReviewRequest
	ExportedFakeIssueComment  = FakeIssueComment
	ExportedFakeThread        = FakeThread
)
