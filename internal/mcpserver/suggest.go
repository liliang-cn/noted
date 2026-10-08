package mcpserver

import (
	"context"
	"fmt"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type listSuggestionsIn struct {
	Space string `json:"space,omitempty" jsonschema:"work or life; omit for both"`
}
type acceptIn struct {
	ID     string            `json:"id"`
	Inputs map[string]string `json:"inputs,omitempty" jsonschema:"values for the proposal's inputs, by name (for example start: 2026-11-20)"`
	Only   []int32           `json:"only,omitempty" jsonschema:"apply only these operations, by position; omit to apply all"`
}
type weeklyIn struct {
	Space string `json:"space,omitempty" jsonschema:"work or life; omit for both"`
}
type textIn struct {
	Text  string `json:"text"`
	Space string `json:"space,omitempty" jsonschema:"work or life"`
}

// registerSuggestions adds the tools around proposals: things somebody suggests
// but has not done. Reading them changes nothing; accepting one applies it.
func (t *tools) registerSuggestions(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "list_suggestions", Description: "Pending suggestions, recomputed from the user's data: goals behind pace, projects with undated tasks, overlapping events, overdue tasks. Each lists the operations it would perform. Nothing is changed by listing.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listSuggestionsIn) (*mcp.CallToolResult, any, error) {
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.sugg.ListProposals(ctx, &pb.ListProposalsRequest{Space: sp})
			if err != nil {
				return nil, nil, err
			}
			return text(r)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "accept_suggestion", Description: "Apply a suggestion (or part of it). This changes the user's data; the result names a change that undo_change can reverse. Proposals that list inputs need them in `inputs`.", Annotations: additive},
		func(ctx context.Context, _ *mcp.CallToolRequest, in acceptIn) (*mcp.CallToolResult, any, error) {
			req := &pb.AcceptProposalRequest{Id: in.ID, Inputs: in.Inputs}
			if len(in.Only) > 0 {
				req.Selection = &pb.OperationSelection{Indexes: in.Only}
			}
			r, err := t.sugg.AcceptProposal(ctx, req)
			if err != nil {
				return nil, nil, err
			}
			return text(r)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "dismiss_suggestion", Description: "Turn a suggestion down; it will not be suggested again.", Annotations: editing},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, any, error) {
			r, err := t.sugg.DismissProposal(ctx, &pb.DismissProposalRequest{Id: in.ID})
			if err != nil {
				return nil, nil, err
			}
			return text(r)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "list_changes", Description: "Recent changes made by accepting suggestions or by the assistant, newest first.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			r, err := t.sugg.ListChanges(ctx, &pb.ListChangesRequest{})
			if err != nil {
				return nil, nil, err
			}
			return text(r)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "undo_change", Description: "Reverse a change from list_changes.", Annotations: editing},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, any, error) {
			r, err := t.sugg.UndoChange(ctx, &pb.UndoChangeRequest{Id: in.ID})
			if err != nil {
				return nil, nil, err
			}
			return text(r)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "weekly_review", Description: "Look back at this week: tasks done, goals, what slipped, and a suggested plan for next week (stored as a pending suggestion).", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in weeklyIn) (*mcp.CallToolResult, any, error) {
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.sugg.GetWeeklyReview(ctx, &pb.GetWeeklyReviewRequest{Space: sp, TimeZone: t.loc.String()})
			if err != nil {
				return nil, nil, err
			}
			return text(r)
		})
}

// registerAIPlanning adds the suggestion tools that need the server's language model.
func (t *tools) registerAIPlanning(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "plan_from_text", Description: "Break a request like \"next month I'm going to X, I need A, B and C\" into a project with tasks. Returns a pending suggestion; dates the user did not give are left as inputs. Apply it with accept_suggestion.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in textIn) (*mcp.CallToolResult, any, error) {
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			r, err := t.ai.PlanFromText(ctx, &pb.PlanFromTextRequest{Text: in.Text, Space: sp})
			if err != nil {
				return nil, nil, err
			}
			return text(r)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "extract_tasks", Description: "Find the to-dos written inside a note and propose them as tasks. Returns a pending suggestion; apply it with accept_suggestion.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, any, error) {
			if in.ID == "" {
				return nil, nil, fmt.Errorf("id is the note's id")
			}
			r, err := t.ai.ExtractTasks(ctx, &pb.ExtractTasksRequest{NoteId: in.ID})
			if err != nil {
				return nil, nil, err
			}
			return text(r)
		})
}
