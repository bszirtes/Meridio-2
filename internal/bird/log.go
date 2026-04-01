package bird

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// BirdLog represents a single BIRD log destination.
// Maps to the BIRD config directive:
//
//	log "filename" [size "backup"] | fixed "filename" size | syslog [name name] | stderr | udp address [port port] all|{ list of classes }
type BirdLog struct {
	Type       string   // "file", "fixed", "syslog", "stderr", "udp"
	Path       string   // file path, syslog name, or UDP address
	Size       int      // rotation limit in bytes (file) or ring buffer size (fixed)
	BackupPath string   // backup file (file mode with rotation)
	Port       int      // UDP port
	Classes    []string // "all" or {"debug", "trace", "info", "remote", "auth", "warning", "error", "bug", "fatal"}
}

// ParseBirdLog parses a colon-separated log specification.
// Formats:
//
//	stderr:classes
//	file:path:classes
//	file:path:size:backup:classes
//	fixed:path:size:classes
//	syslog:name:classes
//	udp:address:port:classes
//
// Classes are comma-separated (e.g. "info,warning,error" or "all").
func ParseBirdLog(s string) (BirdLog, error) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return BirdLog{}, fmt.Errorf("invalid bird log spec %q: need at least type:classes", s)
	}

	typ := parts[0]
	switch typ {
	case "stderr":
		return BirdLog{Type: typ, Classes: parseClasses(parts[1])}, nil

	case "file":
		switch len(parts) {
		case 3:
			return BirdLog{Type: typ, Path: parts[1], Classes: parseClasses(parts[2])}, nil
		case 5:
			size, err := strconv.Atoi(parts[2])
			if err != nil {
				return BirdLog{}, fmt.Errorf("invalid size %q: %w", parts[2], err)
			}
			return BirdLog{Type: typ, Path: parts[1], Size: size, BackupPath: parts[3], Classes: parseClasses(parts[4])}, nil
		default:
			return BirdLog{}, fmt.Errorf("invalid file log spec %q: expected file:path:classes or file:path:size:backup:classes", s)
		}

	case "fixed":
		if len(parts) != 4 {
			return BirdLog{}, fmt.Errorf("invalid fixed log spec %q: expected fixed:path:size:classes", s)
		}
		size, err := strconv.Atoi(parts[2])
		if err != nil {
			return BirdLog{}, fmt.Errorf("invalid size %q: %w", parts[2], err)
		}
		return BirdLog{Type: typ, Path: parts[1], Size: size, Classes: parseClasses(parts[3])}, nil

	case "syslog":
		if len(parts) != 3 {
			return BirdLog{}, fmt.Errorf("invalid syslog log spec %q: expected syslog:name:classes", s)
		}
		return BirdLog{Type: typ, Path: parts[1], Classes: parseClasses(parts[2])}, nil

	case "udp":
		if len(parts) != 4 {
			return BirdLog{}, fmt.Errorf("invalid udp log spec %q: expected udp:address:port:classes", s)
		}
		port, err := strconv.Atoi(parts[2])
		if err != nil {
			return BirdLog{}, fmt.Errorf("invalid port %q: %w", parts[2], err)
		}
		return BirdLog{Type: typ, Path: parts[1], Port: port, Classes: parseClasses(parts[3])}, nil

	default:
		return BirdLog{}, fmt.Errorf("unknown log type %q", typ)
	}
}

// parseClasses splits a comma-separated class list (e.g. "info,warning,error" or "all").
// Duplicates are removed.
func parseClasses(s string) []string {
	var classes []string
	for c := range strings.SplitSeq(s, ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			classes = append(classes, c)
		}
	}
	slices.Sort(classes)
	return slices.Compact(classes)
}

// fmtClasses formats classes for BIRD config syntax.
// Returns "all" if the list contains "all", otherwise "{ info, warning, ... }".
func fmtClasses(classes []string) string {
	if slices.Contains(classes, "all") {
		return "all"
	}
	return "{ " + strings.Join(classes, ", ") + " }"
}

// FmtParams returns the BIRD config line for this log destination.
func (l BirdLog) FmtParams() string {
	classes := fmtClasses(l.Classes)

	switch l.Type {
	case "stderr":
		return fmt.Sprintf("log stderr %s;", classes)
	case "file":
		if l.Size > 0 {
			return fmt.Sprintf("log %q %d %q %s;", l.Path, l.Size, l.BackupPath, classes)
		}
		return fmt.Sprintf("log %q %s;", l.Path, classes)
	case "fixed":
		return fmt.Sprintf("log fixed %q %d %s;", l.Path, l.Size, classes)
	case "syslog":
		return fmt.Sprintf("log syslog name %q %s;", l.Path, classes)
	case "udp":
		return fmt.Sprintf("log udp %s port %d %s;", l.Path, l.Port, classes)
	default:
		return ""
	}
}

// BirdLogParams implements pflag.Value for repeatable --bird-log flags.
type BirdLogParams []BirdLog

func (l *BirdLogParams) String() string {
	return fmt.Sprint(*l)
}

func (l *BirdLogParams) Type() string {
	return "birdlog"
}

func (l *BirdLogParams) Set(val string) error {
	log, err := ParseBirdLog(val)
	if err != nil {
		return err
	}
	*l = append(*l, log)
	return nil
}
