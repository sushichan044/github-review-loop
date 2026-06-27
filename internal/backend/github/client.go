// Package github provides a GitHub adapter that fetches the data needed
// to build a [reviewer.Snapshot].
package github

import (
	"context"

	"github.com/cli/go-gh/v2/pkg/api"
)

// GraphQLQuerier is a testable seam around the GraphQL client.
// The real implementation is [*api.GraphQLClient]; tests pass a fake.
//
// QueryWithContext (not Query) is used so callers can propagate cancellation
// and deadlines down to the network layer.
type GraphQLQuerier interface {
	QueryWithContext(ctx context.Context, name string, q any, variables map[string]any) error
}

// Client holds a GraphQL querier used for all GitHub data fetches.
type Client struct {
	gql GraphQLQuerier
}

// NewClient creates a [Client] backed by the authenticated gh CLI credentials.
func NewClient() (*Client, error) {
	gqlClient, err := api.DefaultGraphQLClient()
	if err != nil {
		return nil, err
	}
	return &Client{gql: gqlClient}, nil
}

// NewClientWithQuerier creates a [Client] backed by the provided querier.
// Intended for use in tests.
func NewClientWithQuerier(q GraphQLQuerier) *Client {
	return &Client{gql: q}
}
