// Package github provides a GitHub adapter that fetches the data needed
// to build a [reviewloop.Snapshot].
package github

import (
	"github.com/cli/go-gh/v2/pkg/api"
)

// GraphQLQuerier is a testable seam around the GraphQL client.
// The real implementation is [*api.GraphQLClient]; tests pass a fake.
type GraphQLQuerier interface {
	Query(name string, q any, variables map[string]any) error
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
