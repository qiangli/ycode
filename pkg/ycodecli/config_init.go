package ycodecli

// Sprint: #387; Story: #1625; Story-ID: 27b3dfdf7e76
//
// Operations config and init. config reports where the effective agent YAML
// came from and validates a candidate file; it never writes settings, so a
// custom file is used only by selecting it (--file, the bootstrap env, or
// /config FILE in the terminal). init creates the repository instruction
// file a declared source names from a declared template, or reports the
// existing one unchanged; it touches no other file.

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
	harnessspec "github.com/qiangli/ycode/internal/harness/spec"
)

func configCLI(doc *harnessspec.Document, inv harnesscli.Invocation, output io.Writer) error {
	switch inv.Dispatch.Action {
	case "source":
		return writeConfigSource(output, doc, inv.ConfigOrigin)
	case "use":
		candidate, err := loadCandidateConfig(inv.Arguments[0])
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "valid: %s\nname: %s\ndigest: %s\n", candidate.Source, candidate.Metadata.Name, candidate.ConfigDigest); err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "select it with --file %s (in the terminal, /config %s switches this terminal to it)\n", candidate.Source, candidate.Source)
		return err
	}
	return fmt.Errorf("unsupported config action %q", inv.Dispatch.Action)
}

// loadCandidateConfig strictly compiles a custom configuration file; any
// failure leaves the effective configuration as it was.
func loadCandidateConfig(file string) (*harnessspec.Document, error) {
	path, err := filepath.Abs(file)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("config %s: not a regular file", path)
	}
	doc, err := harnessspec.Load(path)
	if err != nil {
		return nil, fmt.Errorf("config %s is not used: %w", path, err)
	}
	return doc, nil
}

func writeConfigSource(output io.Writer, doc *harnessspec.Document, origin string) error {
	bootstrap := doc.Spec.Interfaces.CLI.Bootstrap
	var b strings.Builder
	fmt.Fprintf(&b, "config: %s\norigin: %s\nname: %s\ndigest: %s\n", doc.Source, origin, doc.Metadata.Name, doc.ConfigDigest)
	if ref, model, err := defaultHarnessModelResource(doc); err == nil {
		fmt.Fprintf(&b, "agent: %s, model: %s (%s)\n", doc.Spec.Runtime.DefaultAgentRef, ref, model.ID)
	}
	selectors := strings.Join(bootstrap.ConfigFlags, ", ")
	if bootstrap.Env != "" {
		selectors += " FILE or env " + bootstrap.Env
	}
	fmt.Fprintf(&b, "default: %s; select a custom file with %s (in the terminal: /config FILE)\n", bootstrap.DefaultFile, selectors)
	_, err := io.WriteString(output, b.String())
	return err
}

func initCLI(doc *harnessspec.Document, inv harnesscli.Invocation, output io.Writer) error {
	ref := inv.Dispatch.SourceRef
	path, err := doc.SourcePath(ref)
	if err != nil {
		return err
	}
	source := doc.Spec.Sources[ref]
	var status string
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if info.Mode()&fs.ModeSymlink != 0 {
			info, err = os.Stat(path)
			if err != nil {
				return fmt.Errorf("init: %s: %w", path, err)
			}
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("init: %s exists and is not a regular file", path)
		}
		// Existing instructions are the author's: used as they are.
		status = fmt.Sprintf("using: %s (%d bytes, left unchanged)", path, info.Size())
	case errors.Is(err, fs.ErrNotExist):
		if err := doc.WritablePath(path); err != nil {
			return fmt.Errorf("init: %w", err)
		}
		template := doc.Spec.Sources[inv.Dispatch.TemplateRef].Resolved
		if strings.TrimSpace(template) == "" {
			return fmt.Errorf("init: template source %q is empty", inv.Dispatch.TemplateRef)
		}
		if len(template) > source.Limits.MaxBytes {
			return fmt.Errorf("init: template source %q exceeds %s's maxBytes", inv.Dispatch.TemplateRef, ref)
		}
		// O_EXCL: a file that appeared meanwhile is never overwritten.
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("init: %w", err)
		}
		_, werr := io.WriteString(file, template)
		if cerr := file.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return fmt.Errorf("init: %w", werr)
		}
		status = fmt.Sprintf("created: %s (%d bytes)", path, len(template))
	default:
		return fmt.Errorf("init: %s: %w", path, err)
	}
	var loads []string
	for name, context := range doc.Spec.Contexts {
		for _, fragment := range context.Fragments {
			if fragment.SourceRef == ref {
				loads = append(loads, name+"/"+fragment.ID)
			}
		}
	}
	sort.Strings(loads)
	_, err = fmt.Fprintf(output, "%s\ncontext: source %s is loaded by %s when the configuration compiles\nno other file was read or changed\n", status, ref, strings.Join(loads, ", "))
	return err
}
