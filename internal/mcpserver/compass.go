package mcpserver

import (
	"context"
	"fmt"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

type getCompassIn struct {
	Space           string `json:"space,omitempty" jsonschema:"work or life; default life"`
	IncludeRevision bool   `json:"include_history,omitempty" jsonschema:"also list the earlier versions, newest first"`
}
type setCompassIn struct {
	Space       string    `json:"space,omitempty" jsonschema:"work or life; default life"`
	AntiVision  *string   `json:"anti_vision,omitempty" jsonschema:"one sentence: the life I refuse"`
	Vision      *string   `json:"vision,omitempty" jsonschema:"one sentence: what I am building toward"`
	Identity    *string   `json:"identity,omitempty" jsonschema:"I am the kind of person who ... (我是那种……的人)"`
	Constraints *[]string `json:"constraints,omitempty" jsonschema:"what I will not sacrifice; replaces the whole list, [] clears it"`
}

type compassOut struct {
	Space       string   `json:"space"`
	AntiVision  string   `json:"antiVision"`
	Vision      string   `json:"vision"`
	Identity    string   `json:"identity"`
	Constraints []string `json:"constraints"`
	Version     int32    `json:"version" jsonschema:"0: nothing saved yet"`
	UpdateTime  string   `json:"updateTime,omitempty"`
}

// compassFullOut is get_compass's answer: the compass and, on request, its versions.
type compassFullOut struct {
	Space       string        `json:"space"`
	AntiVision  string        `json:"antiVision"`
	Vision      string        `json:"vision"`
	Identity    string        `json:"identity"`
	Constraints []string      `json:"constraints"`
	Version     int32         `json:"version" jsonschema:"0: nothing saved yet"`
	UpdateTime  string        `json:"updateTime,omitempty"`
	History     []*compassOut `json:"history,omitempty" jsonschema:"earlier versions, newest first; only with include_history"`
}

func (t *tools) compass(c *pb.Compass) *compassOut {
	if c == nil {
		return nil
	}
	cs := c.Constraints
	if cs == nil {
		cs = []string{}
	}
	return &compassOut{Space: spaceName(c.Space), AntiVision: c.AntiVision, Vision: c.Vision, Identity: c.Identity,
		Constraints: cs, Version: c.Version, UpdateTime: t.ts(c.UpdateTime)}
}

// registerCompass adds the compass: the sentences above every objective and goal.
func (t *tools) registerCompass(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "get_compass", Description: "The compass (罗盘) of a space: the life the person refuses (anti_vision), what they are building toward (vision), who they are (identity) and what they will not sacrifice (constraints). It sits above objectives and goals; read it before proposing goals or plans. Version 0 means none was written yet.", Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in getCompassIn) (*mcp.CallToolResult, *compassFullOut, error) {
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			c, err := t.compassC.GetCompass(ctx, &pb.GetCompassRequest{Space: sp})
			if err != nil {
				return nil, nil, err
			}
			o := t.compass(c)
			out := &compassFullOut{Space: o.Space, AntiVision: o.AntiVision, Vision: o.Vision, Identity: o.Identity, Constraints: o.Constraints, Version: o.Version, UpdateTime: o.UpdateTime}
			if in.IncludeRevision {
				h, err := t.compassC.ListCompassRevisions(ctx, &pb.ListCompassRevisionsRequest{Space: sp})
				if err != nil {
					return nil, nil, err
				}
				out.History = items(h.Revisions, t.compass)
			}
			return reply(c, out)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "set_compass", Description: "Write the compass of a space. Omitted fields keep their current value; every change is saved as a new version, so the compass can evolve and earlier versions stay in get_compass history. Keep anti_vision and vision to one sentence each.", Annotations: editing},
		func(ctx context.Context, _ *mcp.CallToolRequest, in setCompassIn) (*mcp.CallToolResult, *compassOut, error) {
			sp, err := spaceOf(in.Space)
			if err != nil {
				return nil, nil, err
			}
			c, mask := &pb.Compass{Space: sp}, []string{}
			if in.AntiVision != nil {
				c.AntiVision, mask = *in.AntiVision, append(mask, "anti_vision")
			}
			if in.Vision != nil {
				c.Vision, mask = *in.Vision, append(mask, "vision")
			}
			if in.Identity != nil {
				c.Identity, mask = *in.Identity, append(mask, "identity")
			}
			if in.Constraints != nil {
				c.Constraints, mask = *in.Constraints, append(mask, "constraints")
			}
			if len(mask) == 0 {
				return nil, nil, fmt.Errorf("nothing to update: pass at least one field")
			}
			r, err := t.compassC.SetCompass(ctx, &pb.SetCompassRequest{Compass: c, UpdateMask: &fieldmaskpb.FieldMask{Paths: mask}})
			if err != nil {
				return nil, nil, err
			}
			return reply(r, t.compass(r))
		})
}
