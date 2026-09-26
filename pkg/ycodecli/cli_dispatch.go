package ycodecli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	harnessacp "github.com/qiangli/ycode/internal/harness/acp"
	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/frontend"
	harnessspec "github.com/qiangli/ycode/internal/harness/spec"
	public "github.com/qiangli/ycode/pkg/ycode"
)

// Operations are neutral execution mechanisms. Command names, hierarchy,
// flags, help and routes are supplied exclusively by the compiled document.
func dispatchCLI(ctx context.Context, inv harnesscli.Invocation, streams harnesscli.IO) error {
	file := inv.ConfigFile
	switch inv.Dispatch.Operation {
	case "version":
		_, err := fmt.Fprintf(streams.Out, "%s %s (%s)\n", inv.Command[0], version, commit)
		return err
	case "schema":
		data, err := harnessspec.JSONSchema()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(streams.Out, string(data))
		return err
	case "docs":
		return runDocsInvocation(inv, streams)
	case "features":
		return runFeatureInvocation(inv, streams)
	case "acp":
		return runACPInvocation(file, inv.Dispatch, streams)
	case "shell":
		return runHarnessShellInvocation(ctx, inv, streams)
	}
	doc, err := loadHarness(file)
	if err != nil {
		return err
	}
	switch inv.Dispatch.Operation {
	case "validate":
		_, err := fmt.Fprintf(streams.Out, "valid: %s (%d agents, %d pipelines)\n", doc.Metadata.Name, len(doc.Agents), len(doc.Pipelines))
		return err
	case "readiness":
		message, ready := harnessCredentialStatus(doc)
		status := "BLOCKED"
		if ready {
			status = "READY"
		}
		_, err := fmt.Fprintf(streams.Out, "provider\t%s\t%s\nconfig\tREADY\t%s (%s)\n", status, message, doc.Source, doc.ConfigDigest)
		return err
	case "inspect":
		return inspectCLI(doc, inv, streams.Out)
	case "session":
		app, err := openHarnessApplication(file)
		if err != nil {
			return err
		}
		defer app.Close()
		return sessionCLI(ctx, app, inv, streams.Out)
	case "input", "serve":
		app, err := openHarnessApplication(file)
		if err != nil {
			return err
		}
		defer app.Close()
		if inv.Dispatch.Operation == "serve" {
			return app.ServeRoute(ctx, inv.Dispatch, streams.Err)
		}
		return runCLIInput(ctx, app, inv, streams)
	default:
		return fmt.Errorf("unsupported CLI operation %q", inv.Dispatch.Operation)
	}
}

func stringFlag(inv harnesscli.Invocation, name string) string {
	value, _ := inv.Flags[name].(string)
	return value
}

func boolFlag(inv harnesscli.Invocation, name string) bool {
	value, _ := inv.Flags[name].(bool)
	return value
}

func inspectCLI(doc *harnessspec.Document, inv harnesscli.Invocation, output io.Writer) error {
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	var values map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	resource := inv.Dispatch.Resource
	var value any = values
	if resource != "document" {
		var ok bool
		value, ok = getDotted(values, "spec."+resource)
		if !ok {
			return fmt.Errorf("compiled harness resource not found: %s", resource)
		}
	}
	switch inv.Dispatch.Action {
	case "path":
		_, err := fmt.Fprintln(output, doc.Source)
		return err
	case "get":
		object, ok := value.(map[string]any)
		if !ok {
			return errors.New("compiled resource is not an object")
		}
		value, ok = getDotted(object, inv.Arguments[0])
		if !ok {
			return fmt.Errorf("compiled harness key not found: %s", inv.Arguments[0])
		}
	case "current":
		current, err := defaultHarnessModel(doc)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, current)
		return err
	case "show":
		if len(inv.Arguments) != 0 {
			object, ok := value.(map[string]any)
			if !ok {
				return errors.New("compiled resource is not an object")
			}
			value, ok = object[inv.Arguments[0]]
			if !ok {
				return fmt.Errorf("compiled resource not found: %s", inv.Arguments[0])
			}
		}
	case "list":
		if resource == "bashy" {
			return inspectBashy(doc, inv, output)
		}
		if !boolFlag(inv, "json") {
			object, ok := value.(map[string]any)
			if !ok {
				return errors.New("compiled resource is not a collection")
			}
			names := make([]string, 0, len(object))
			for name := range object {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				cells := []string{name}
				row, _ := object[name].(map[string]any)
				for _, column := range inv.Dispatch.Columns {
					field, ok := getDotted(row, column)
					if !ok {
						return fmt.Errorf("compiled resource column not found: %s", column)
					}
					cells = append(cells, tableCell(field))
				}
				if _, err := fmt.Fprintln(output, strings.Join(cells, "\t")); err != nil {
					return err
				}
			}
			return nil
		}
	default:
		return fmt.Errorf("unsupported inspection action %q", inv.Dispatch.Action)
	}
	if text, ok := value.(string); ok {
		_, err := fmt.Fprintln(output, text)
		return err
	}
	return writeJSON(output, value)
}

func tableCell(value any) string {
	if list, ok := value.([]any); ok {
		parts := make([]string, len(list))
		for i, item := range list {
			parts[i] = fmt.Sprint(item)
		}
		return strings.Join(parts, ",")
	}
	return fmt.Sprint(value)
}

func writeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func inspectBashy(doc *harnessspec.Document, inv harnesscli.Invocation, output io.Writer) error {
	resource := doc.Spec.Bashy
	tool := configuredTool{
		Name: "bashy", Contract: resource.Contract, RequestType: resource.RequestType,
		ResultType: resource.ResultType, Operations: append([]string(nil), resource.Operations...),
		Effects:    append([]string(nil), resource.Execution.EffectsCeiling...),
		Permission: resource.Execution.PermissionCeiling,
		Preflight:  resource.Execution.Preflight, HostFallback: resource.Execution.HostFallback,
	}
	sort.Strings(tool.Operations)
	sort.Strings(tool.Effects)
	if boolFlag(inv, "json") {
		return writeJSON(output, []configuredTool{tool})
	}
	if len(inv.Dispatch.Columns) > 0 {
		data, err := json.Marshal(resource)
		if err != nil {
			return err
		}
		var row map[string]any
		if err := json.Unmarshal(data, &row); err != nil {
			return err
		}
		cells := []string{tool.Name}
		for _, column := range inv.Dispatch.Columns {
			value, ok := getDotted(row, column)
			if !ok {
				return fmt.Errorf("compiled resource column not found: %s", column)
			}
			cells = append(cells, tableCell(value))
		}
		_, err = fmt.Fprintln(output, strings.Join(cells, "\t"))
		return err
	}
	if _, err := fmt.Fprintf(output, "%s\t%s\t%s\t%s\n", tool.Name, tool.Contract, tool.Permission, tool.Preflight); err != nil {
		return err
	}
	_, err := fmt.Fprintf(output, "operations\t%s\n", strings.Join(tool.Operations, ","))
	return err
}

type cliController struct {
	*harnessApplication
	agentRef string
}

func (c cliController) Submit(ctx context.Context, input frontend.Input) (<-chan event.Event, error) {
	input.AgentRef = c.agentRef
	return c.harnessApplication.Submit(ctx, input)
}

func runCLIInput(ctx context.Context, app *harnessApplication, inv harnesscli.Invocation, streams harnesscli.IO) error {
	ref := inv.FrontendRef
	trigger, err := app.triggerFor(ref)
	if err != nil {
		return err
	}
	if trigger != inv.Dispatch.TriggerRef {
		return errors.New("CLI dispatch route differs from compiled trigger")
	}
	local, err := frontend.NewLocal(app.doc, ref, cliController{app, inv.Dispatch.AgentRef})
	if err != nil {
		return err
	}
	defaults := app.defaults()
	if session := stringFlag(inv, inv.Dispatch.Input.SessionFlag); session != "" {
		found, err := app.harness.Session(session)
		switch {
		case err == nil:
			defaults.SessionID = found.ID
		case strings.HasPrefix(err.Error(), "no session"):
			defaults.SessionID = session // a new session under a caller-chosen id
		default:
			return err
		}
	} else if inv.Dispatch.Input.SessionDefault == "latest" {
		sessions, err := app.harness.Sessions()
		if err != nil {
			return err
		}
		if len(sessions) == 0 {
			return &harnesscli.Error{Class: "usage", Code: app.doc.Spec.Interfaces.CLI.ExitCodes.Usage, Err: errors.New("no session to resume")}
		}
		defaults.SessionID = sessions[0].ID
	}
	submit := func(text string) error {
		if strings.TrimSpace(text) == "" {
			if inv.Dispatch.Input.Empty == "help" && streams.Help != nil {
				return streams.Help()
			}
			return &harnesscli.Error{Class: "usage", Code: app.doc.Spec.Interfaces.CLI.ExitCodes.Usage, Err: errors.New("prompt is empty")}
		}
		body, err := json.Marshal(map[string]string{inv.Dispatch.Input.PayloadKey: text})
		if err != nil {
			return err
		}
		return local.Run(ctx, frontend.Input{SessionID: defaults.SessionID, Principal: defaults.Principal, Body: body}, app.renderer(streams.Out))
	}
	switch inv.Mode {
	case "args":
		return submit(strings.Join(inv.Arguments, " "))
	case "stdin":
		limit := app.doc.Spec.Frontends[ref].Limits.MaxInputBytes
		body, err := io.ReadAll(io.LimitReader(streams.In, int64(limit)+1))
		if err != nil {
			return err
		}
		if len(body) > limit {
			return fmt.Errorf("frontend input exceeds %d bytes", limit)
		}
		return submit(strings.TrimSuffix(string(body), "\n"))
	case "repl":
		scanner := bufio.NewScanner(streams.In)
		scanner.Buffer(make([]byte, 4096), app.doc.Spec.Frontends[ref].Limits.MaxInputBytes)
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) != "" {
				if err := submit(scanner.Text()); err != nil {
					return err
				}
			}
		}
		return scanner.Err()
	default:
		return fmt.Errorf("unsupported input mode %q", inv.Mode)
	}
}

func runACPInvocation(file string, dispatch harnessspec.CLIDispatch, streams harnesscli.IO) error {
	doc, err := loadHarness(file)
	if err != nil {
		return err
	}
	store, err := harnessacp.Open(filepath.Join(harnessspec.ControlRootPath(doc), "acp-sessions.json"))
	if err != nil {
		return err
	}
	return serveACPWithRoute(streams.In, streams.Out, streams.Err, func(cwd string) (harnessApp, error) {
		path := file
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		return public.Load(path)
	}, dispatch, store)
}
