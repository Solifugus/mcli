package core

import (
	"context"
	"errors"
	"testing"
)

func TestJobOpsRequireConnection(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()
	if _, err := c.ListJobs(ctx, ""); !errors.Is(err, ErrNotConnected) {
		t.Errorf("ListJobs without connection = %v, want ErrNotConnected", err)
	}
	if _, err := c.DescribeJob(ctx, "j"); !errors.Is(err, ErrNotConnected) {
		t.Errorf("DescribeJob without connection = %v, want ErrNotConnected", err)
	}
	if _, err := c.JobHistory(ctx, "j", 0); !errors.Is(err, ErrNotConnected) {
		t.Errorf("JobHistory without connection = %v, want ErrNotConnected", err)
	}
}
