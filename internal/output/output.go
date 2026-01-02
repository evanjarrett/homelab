package output

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/fatih/color"
)

var (
	// Color functions
	blue   = color.New(color.FgBlue).SprintFunc()
	green  = color.New(color.FgGreen).SprintFunc()
	yellow = color.New(color.FgYellow).SprintFunc()
	red    = color.New(color.FgRed).SprintFunc()

	// Output destination (can be changed for testing)
	Out io.Writer = os.Stdout
	Err io.Writer = os.Stderr
)

// LogInfo prints an info message with blue [INFO] prefix
func LogInfo(format string, args ...interface{}) {
	fmt.Fprintf(Out, "%s %s\n", blue("[INFO]"), fmt.Sprintf(format, args...))
}

// LogSuccess prints a success message with green [OK] prefix
func LogSuccess(format string, args ...interface{}) {
	fmt.Fprintf(Out, "%s %s\n", green("[OK]"), fmt.Sprintf(format, args...))
}

// LogWarn prints a warning message with yellow [WARN] prefix
func LogWarn(format string, args ...interface{}) {
	fmt.Fprintf(Out, "%s %s\n", yellow("[WARN]"), fmt.Sprintf(format, args...))
}

// LogError prints an error message with red [ERROR] prefix
func LogError(format string, args ...interface{}) {
	fmt.Fprintf(Err, "%s %s\n", red("[ERROR]"), fmt.Sprintf(format, args...))
}

// Header prints a header line
func Header(format string, args ...interface{}) {
	fmt.Fprintf(Out, "=== %s ===\n", fmt.Sprintf(format, args...))
}

// SubHeader prints a sub-header line
func SubHeader(format string, args ...interface{}) {
	fmt.Fprintf(Out, "--- %s ---\n", fmt.Sprintf(format, args...))
}

// Separator prints a separator line
func Separator() {
	fmt.Fprintln(Out, "============================================")
}

// Print prints a plain message
func Print(format string, args ...interface{}) {
	fmt.Fprintf(Out, format, args...)
}

// Println prints a plain message with newline
func Println(format string, args ...interface{}) {
	fmt.Fprintf(Out, format+"\n", args...)
}

// NewTabWriter creates a new tabwriter for formatted table output
func NewTabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(Out, 0, 0, 2, ' ', 0)
}

// StatusColor returns the appropriate color function for a status
func StatusColor(status string) func(a ...interface{}) string {
	switch status {
	case "OK", "Ready", "true":
		return green
	case "UNREACHABLE", "NotReady", "false":
		return red
	default:
		return yellow
	}
}

// RoleColor returns a color function for node roles
func RoleColor(role string) func(a ...interface{}) string {
	switch role {
	case "controlplane":
		return color.New(color.FgMagenta).SprintFunc()
	case "worker":
		return color.New(color.FgCyan).SprintFunc()
	default:
		return color.New(color.FgWhite).SprintFunc()
	}
}

// ProgressDot prints a progress dot (for waiting loops)
func ProgressDot() {
	fmt.Fprint(Out, ".")
}

// ProgressNewline ends a progress line
func ProgressNewline() {
	fmt.Fprintln(Out)
}
