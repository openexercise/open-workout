package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// tools.json is a copy of the published profile (../tools.json; the publish script writes
// both). The server lists the core tools exactly as the profile has them, so a client sees the
// same names, descriptions, schemas and annotations from this server as from any other.
//
//go:embed tools.json
var profileJSON []byte

// profileKey is the _meta key naming the profile a tool belongs to.
const profileKey = "dev.openworkout/profile"

type profile struct {
	Instructions string      `json:"instructions"`
	Tools        []*mcp.Tool `json:"tools"`
}

// coreProfile returns the profile's instructions and its core tools.
func coreProfile() (string, []*mcp.Tool, error) {
	var p profile
	if err := json.Unmarshal(profileJSON, &p); err != nil {
		return "", nil, fmt.Errorf("read tools.json: %w", err)
	}
	core := slices.DeleteFunc(p.Tools, func(t *mcp.Tool) bool { return t.Meta[profileKey] != "core" })
	return p.Instructions, core, nil
}

// call is what a tool handler gets: whose log, which client and the raw arguments.
type call struct {
	person string
	client string
	args   json.RawMessage
}

type handler func(s *Store, c call) (Result, bool, error)

// handlers implement the core tools. Writes go through write, which applies idempotency_key;
// reads through read.
var handlers = map[string]handler{
	"log_workout": func(s *Store, c call) (Result, bool, error) {
		in, err := decode[LogWorkoutInput](c.args)
		if err != nil {
			return nil, false, err
		}
		return write(s, c, "log_workout", in.IdempotencyKey, func(l *personLog) (Result, error) { return l.LogWorkout(in, c.client) })
	},
	"add_sets": func(s *Store, c call) (Result, bool, error) {
		in, err := decode[AddSetsInput](c.args)
		if err != nil {
			return nil, false, err
		}
		return write(s, c, "add_sets", in.IdempotencyKey, func(l *personLog) (Result, error) { return l.AddSets(in, c.client) })
	},
	"update_entry": func(s *Store, c call) (Result, bool, error) {
		in, err := decode[UpdateInput](c.args)
		if err != nil {
			return nil, false, err
		}
		return write(s, c, "update_entry", "", func(l *personLog) (Result, error) { return l.Update(in) })
	},
	"delete_entry": func(s *Store, c call) (Result, bool, error) {
		in, err := decode[struct {
			EntryID string `json:"entry_id"`
		}](c.args)
		if err != nil {
			return nil, false, err
		}
		return write(s, c, "delete_entry", "", func(l *personLog) (Result, error) { return l.Delete(in.EntryID) })
	},
	"undo": func(s *Store, c call) (Result, bool, error) {
		in, err := decode[struct {
			UndoToken string `json:"undo_token"`
		}](c.args)
		if err != nil {
			return nil, false, err
		}
		return write(s, c, "undo", "", func(l *personLog) (Result, error) { return l.Undo(in.UndoToken) })
	},
	"get_workouts": func(s *Store, c call) (Result, bool, error) {
		in, err := decode[struct {
			From     string `json:"from"`
			To       string `json:"to"`
			Exercise string `json:"exercise"`
			Limit    int    `json:"limit"`
		}](c.args)
		if err != nil {
			return nil, false, err
		}
		return read(s, c, func(l *personLog) (Result, error) { return l.Workouts(in.From, in.To, in.Exercise, in.Limit) })
	},
	"get_personal_records": func(s *Store, c call) (Result, bool, error) {
		in, err := decode[struct {
			Exercise string `json:"exercise"`
		}](c.args)
		if err != nil {
			return nil, false, err
		}
		return read(s, c, func(l *personLog) (Result, error) { return l.Records(in.Exercise) })
	},
	"get_progress": func(s *Store, c call) (Result, bool, error) {
		in, err := decode[struct {
			Exercise string `json:"exercise"`
			Metric   string `json:"metric"`
			From     string `json:"from"`
			To       string `json:"to"`
		}](c.args)
		if err != nil {
			return nil, false, err
		}
		return read(s, c, func(l *personLog) (Result, error) { return l.Progress(in.Exercise, in.Metric, in.From, in.To) })
	},
	"search_exercises": func(s *Store, c call) (Result, bool, error) {
		in, err := decode[struct {
			Query     string `json:"query"`
			Equipment string `json:"equipment"`
			Muscle    string `json:"muscle"`
			Limit     int    `json:"limit"`
		}](c.args)
		if err != nil {
			return nil, false, err
		}
		return read(s, c, func(l *personLog) (Result, error) { return l.Search(in.Query, in.Equipment, in.Muscle, in.Limit) })
	},
}

// write runs a change. Its fingerprint is the tool and its arguments in a canonical form, so a
// retry that orders fields differently is still the same request.
func write(s *Store, c call, tool, key string, fn func(l *personLog) (Result, error)) (Result, bool, error) {
	var args any
	if err := json.Unmarshal(c.args, &args); err != nil {
		return nil, false, fail("invalid_input", "arguments: %v", err)
	}
	canonical, err := json.Marshal(args)
	if err != nil {
		return nil, false, err
	}
	return s.Write(c.person, key, tool+" "+string(canonical), fn)
}

func read(s *Store, c call, fn func(l *personLog) (Result, error)) (Result, bool, error) {
	res, err := s.Read(c.person, fn)
	return res, false, err
}

// decode reads a tool's arguments strictly: an unknown field is invalid_input, as the
// profile's schemas say (additionalProperties: false).
func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 {
		return v, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, fail("invalid_input", "arguments: %v", err)
	}
	return v, nil
}

// NewServer serves the profile's core tools over a store. It fails if the profile names a core
// tool this server doesn't handle, so a profile update can't slip by.
func NewServer(store *Store) (*mcp.Server, error) {
	instructions, tools, err := coreProfile()
	if err != nil {
		return nil, err
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "open-workout-template", Version: "0.1.0"},
		&mcp.ServerOptions{Instructions: instructions})
	for _, t := range tools {
		h, ok := handlers[t.Name]
		if !ok {
			return nil, fmt.Errorf("the profile's core has %s, which this server doesn't handle", t.Name)
		}
		srv.AddTool(t, toolHandler(store, h))
	}
	if len(tools) != len(handlers) {
		return nil, fmt.Errorf("this server handles %d tools, but the profile's core has %d", len(handlers), len(tools))
	}
	return srv, nil
}

// toolHandler runs a handler for the person the request's credential names, and returns its
// result the way the profile says: the summary, then the JSON, plus structured content. A
// failure is a tool error whose text starts with its code.
func toolHandler(store *Store, h handler) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c := call{person: personOf(req), client: clientOf(req), args: req.Params.Arguments}
		res, replayed, err := h(store, c)
		if err != nil {
			var te *ToolError
			if !errors.As(err, &te) {
				te = &ToolError{Code: "internal_error", Message: "something went wrong on the server's side; try again"}
			}
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: te.Error()}}}, nil
		}
		raw, err := json.Marshal(res)
		if err != nil {
			return nil, err
		}
		var structured map[string]any
		if err := json.Unmarshal(raw, &structured); err != nil {
			return nil, err
		}
		if replayed {
			structured["replayed"] = true
			if raw, err = json.Marshal(structured); err != nil {
				return nil, err
			}
		}
		return &mcp.CallToolResult{
			Content:           []mcp.Content{&mcp.TextContent{Text: res.summary()}, &mcp.TextContent{Text: string(raw)}},
			StructuredContent: structured,
		}, nil
	}
}

// personOf is whose log a call reads and writes: the person the bearer token names. Without
// authentication (tests over an in-memory transport) it's one local person.
func personOf(req *mcp.CallToolRequest) string {
	if req.Extra != nil && req.Extra.TokenInfo != nil && req.Extra.TokenInfo.UserID != "" {
		return req.Extra.TokenInfo.UserID
	}
	return "local"
}

// clientOf names the client in each workout's source, from what it said when it connected.
func clientOf(req *mcp.CallToolRequest) string {
	if req.Session == nil {
		return ""
	}
	if p := req.Session.InitializeParams(); p != nil && p.ClientInfo != nil {
		return p.ClientInfo.Name
	}
	return ""
}
