package aoiclient

import (
	"context"

	"github.com/go-resty/resty/v2"
)

const DefaultUA = "lfs-auto-grader/v0.1.0-alpha"

type Client struct {
	r *resty.Client
}

func New(addr string) *Client {
	return &Client{
		r: resty.New().SetBaseURL(addr).SetHeader("User-Agent", DefaultUA),
	}
}

func (c *Client) SetUA(ua string) *Client {
	c.r.SetHeader("User-Agent", ua)
	return c
}

func (c *Client) Authenticate(id string, key string) *Client {
	c.r.SetHeader("X-AOI-Runner-Id", id).SetHeader("X-AOI-Runner-Key", key)
	return c
}

func (c *Client) Register(
	ctx context.Context,
	name string, labels []string,
	version string, token string,
) (
	id, key string, err error,
) {
	req := &registerRequest{
		Name:              name,
		Labels:            labels,
		Version:           version,
		RegistrationToken: token,
	}
	res, err := register(ctx, c.r, req)
	if err != nil {
		return "", "", err
	}
	return res.RunnerId, res.RunnerKey, nil
}

func (c *Client) Poll(ctx context.Context) (*SolutionPoll, error) {
	res, err := pollSolution(ctx, c.r)
	if err != nil {
		return nil, err
	}
	return res, nil
}

type SolutionClient struct {
	taskID     string
	solutionID string

	c *Client
}

func (c *Client) Solution(solutionID string, taskID string) *SolutionClient {
	return &SolutionClient{
		taskID:     taskID,
		solutionID: solutionID,
		c:          c,
	}
}

func (sc *SolutionClient) TaskID() string {
	return sc.taskID
}

func (sc *SolutionClient) SolutionID() string {
	return sc.solutionID
}

func (sc *SolutionClient) Patch(ctx context.Context, info *SolutionInfo) error {
	return patchSolutionTask(ctx, sc.c.r, sc.solutionID, sc.taskID, info)
}

func (sc *SolutionClient) Complete(ctx context.Context) error {
	return completeSolutionTask(ctx, sc.c.r, sc.solutionID, sc.taskID)
}

func (sc *SolutionClient) SaveDetails(ctx context.Context, details *SolutionDetails) error {
	return saveSolutionDetails(ctx, sc.c.r, sc.solutionID, sc.taskID, details)
}
