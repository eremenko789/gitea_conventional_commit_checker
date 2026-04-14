package conventional

import (
	"regexp"
	"strings"
	"unicode"
)

var subjectLine = regexp.MustCompile(`^([a-zA-Z0-9]+)(\([^()]*\))?(!)?: (.+)$`)

// Result describes validation outcome for one commit subject line.
type Result struct {
	OK     bool
	Reason string
}

// ValidateSubject checks the first line of a commit message against Conventional Commits 1.0.0-style rules.
// allowedTypes maps lowercased type tokens to struct{}; skipMerge when true skips lines matching merge/revert heuristics.
func ValidateSubject(firstLine string, allowedTypes map[string]struct{}, skipMerge bool) Result {
	line := strings.TrimSpace(strings.Split(firstLine, "\n")[0])
	line = strings.TrimRight(line, "\r")
	if line == "" {
		return Result{OK: false, Reason: "empty subject"}
	}
	if skipMerge && isMergeOrRevertSubject(line) {
		return Result{OK: true, Reason: "skipped merge/revert style"}
	}
	m := subjectLine.FindStringSubmatch(line)
	if m == nil {
		return Result{OK: false, Reason: "subject does not match <type>[scope][!]: <description>"}
	}
	typ := strings.ToLower(m[1])
	if _, ok := allowedTypes[typ]; !ok {
		return Result{OK: false, Reason: "type not allowed: " + typ}
	}
	desc := m[4]
	desc = strings.TrimSpace(desc)
	if desc == "" {
		return Result{OK: false, Reason: "empty description after colon"}
	}
	if !hasNonSpace(desc) {
		return Result{OK: false, Reason: "empty description after colon"}
	}
	return Result{OK: true}
}

func hasNonSpace(s string) bool {
	for _, r := range s {
		if !unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

func isMergeOrRevertSubject(line string) bool {
	if strings.HasPrefix(line, "Merge ") {
		return true
	}
	if strings.HasPrefix(line, "Revert ") {
		return true
	}
	return false
}
