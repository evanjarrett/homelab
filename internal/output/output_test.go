package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureOutput captures stdout and stderr during a test
func captureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()

	oldOut := Out
	oldErr := Err
	defer func() {
		Out = oldOut
		Err = oldErr
	}()

	var outBuf, errBuf bytes.Buffer
	Out = &outBuf
	Err = &errBuf

	fn()

	return outBuf.String(), errBuf.String()
}

// ============================================================================
// LogInfo() Tests
// ============================================================================

func TestLogInfo_SimpleMessage(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		LogInfo("test message")
	})

	assert.Contains(t, stdout, "[INFO]")
	assert.Contains(t, stdout, "test message")
	assert.True(t, strings.HasSuffix(stdout, "\n"))
}

func TestLogInfo_Formatted(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		LogInfo("node %s version %s", "192.168.1.1", "1.7.0")
	})

	assert.Contains(t, stdout, "[INFO]")
	assert.Contains(t, stdout, "node 192.168.1.1 version 1.7.0")
}

// ============================================================================
// LogSuccess() Tests
// ============================================================================

func TestLogSuccess_SimpleMessage(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		LogSuccess("operation complete")
	})

	assert.Contains(t, stdout, "[OK]")
	assert.Contains(t, stdout, "operation complete")
}

func TestLogSuccess_Formatted(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		LogSuccess("upgraded %d nodes", 5)
	})

	assert.Contains(t, stdout, "upgraded 5 nodes")
}

// ============================================================================
// LogWarn() Tests
// ============================================================================

func TestLogWarn_SimpleMessage(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		LogWarn("this is a warning")
	})

	assert.Contains(t, stdout, "[WARN]")
	assert.Contains(t, stdout, "this is a warning")
}

func TestLogWarn_Formatted(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		LogWarn("node %s is unreachable", "192.168.1.99")
	})

	assert.Contains(t, stdout, "node 192.168.1.99 is unreachable")
}

// ============================================================================
// LogError() Tests
// ============================================================================

func TestLogError_SimpleMessage(t *testing.T) {
	_, stderr := captureOutput(t, func() {
		LogError("something failed")
	})

	assert.Contains(t, stderr, "[ERROR]")
	assert.Contains(t, stderr, "something failed")
}

func TestLogError_Formatted(t *testing.T) {
	_, stderr := captureOutput(t, func() {
		LogError("failed to connect to %s: %v", "192.168.1.1", "connection refused")
	})

	assert.Contains(t, stderr, "failed to connect to 192.168.1.1: connection refused")
}

// ============================================================================
// Header() Tests
// ============================================================================

func TestHeader_SimpleMessage(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		Header("Talos Upgrade")
	})

	assert.Equal(t, "=== Talos Upgrade ===\n", stdout)
}

func TestHeader_Formatted(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		Header("Upgrade to v%s", "1.7.0")
	})

	assert.Equal(t, "=== Upgrade to v1.7.0 ===\n", stdout)
}

// ============================================================================
// SubHeader() Tests
// ============================================================================

func TestSubHeader_SimpleMessage(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		SubHeader("Node Status")
	})

	assert.Equal(t, "--- Node Status ---\n", stdout)
}

func TestSubHeader_Formatted(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		SubHeader("Processing %d nodes", 10)
	})

	assert.Equal(t, "--- Processing 10 nodes ---\n", stdout)
}

// ============================================================================
// Separator() Tests
// ============================================================================

func TestSeparator(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		Separator()
	})

	assert.Equal(t, "============================================\n", stdout)
}

// ============================================================================
// Print() and Println() Tests
// ============================================================================

func TestPrint_NoNewline(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		Print("no newline")
	})

	assert.Equal(t, "no newline", stdout)
	assert.False(t, strings.HasSuffix(stdout, "\n"))
}

func TestPrint_Formatted(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		Print("value: %d", 42)
	})

	assert.Equal(t, "value: 42", stdout)
}

func TestPrintln_WithNewline(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		Println("with newline")
	})

	assert.Equal(t, "with newline\n", stdout)
}

func TestPrintln_Formatted(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		Println("count: %d", 100)
	})

	assert.Equal(t, "count: 100\n", stdout)
}

// ============================================================================
// NewTabWriter() Tests
// ============================================================================

func TestNewTabWriter(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		tw := NewTabWriter()
		require.NotNil(t, tw)

		tw.Write([]byte("column1\tcolumn2\tcolumn3\n"))
		tw.Write([]byte("a\tb\tc\n"))
		tw.Flush()
	})

	// Verify tabwriter produced aligned output
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	require.Len(t, lines, 2)

	// Both lines should have consistent spacing (tabwriter adds padding)
	assert.Contains(t, stdout, "column1")
	assert.Contains(t, stdout, "column2")
	assert.Contains(t, stdout, "column3")
}

// ============================================================================
// StatusColor() Tests
// ============================================================================

func TestStatusColor_OK(t *testing.T) {
	colorFn := StatusColor("OK")
	assert.NotNil(t, colorFn)

	// Verify it produces some output (color codes may vary)
	result := colorFn("test")
	assert.NotEmpty(t, result)
}

func TestStatusColor_Ready(t *testing.T) {
	colorFn := StatusColor("Ready")
	assert.NotNil(t, colorFn)
}

func TestStatusColor_True(t *testing.T) {
	colorFn := StatusColor("true")
	assert.NotNil(t, colorFn)
}

func TestStatusColor_Unreachable(t *testing.T) {
	colorFn := StatusColor("UNREACHABLE")
	assert.NotNil(t, colorFn)
}

func TestStatusColor_NotReady(t *testing.T) {
	colorFn := StatusColor("NotReady")
	assert.NotNil(t, colorFn)
}

func TestStatusColor_False(t *testing.T) {
	colorFn := StatusColor("false")
	assert.NotNil(t, colorFn)
}

func TestStatusColor_Unknown(t *testing.T) {
	colorFn := StatusColor("unknown")
	assert.NotNil(t, colorFn)
}

// ============================================================================
// RoleColor() Tests
// ============================================================================

func TestRoleColor_ControlPlane(t *testing.T) {
	colorFn := RoleColor("controlplane")
	assert.NotNil(t, colorFn)

	result := colorFn("controlplane")
	assert.NotEmpty(t, result)
}

func TestRoleColor_Worker(t *testing.T) {
	colorFn := RoleColor("worker")
	assert.NotNil(t, colorFn)

	result := colorFn("worker")
	assert.NotEmpty(t, result)
}

func TestRoleColor_Unknown(t *testing.T) {
	colorFn := RoleColor("unknown")
	assert.NotNil(t, colorFn)

	result := colorFn("unknown")
	assert.NotEmpty(t, result)
}

// ============================================================================
// ProgressDot() and ProgressNewline() Tests
// ============================================================================

func TestProgressDot(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		ProgressDot()
		ProgressDot()
		ProgressDot()
	})

	assert.Equal(t, "...", stdout)
}

func TestProgressNewline(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		ProgressDot()
		ProgressDot()
		ProgressNewline()
	})

	assert.Equal(t, "..\n", stdout)
}

// ============================================================================
// Output Isolation Tests
// ============================================================================

func TestOutput_ErrorGoesToStderr(t *testing.T) {
	stdout, stderr := captureOutput(t, func() {
		LogInfo("info message")
		LogSuccess("success message")
		LogWarn("warning message")
		LogError("error message")
	})

	// Info, Success, and Warn should go to stdout
	assert.Contains(t, stdout, "[INFO]")
	assert.Contains(t, stdout, "[OK]")
	assert.Contains(t, stdout, "[WARN]")
	assert.NotContains(t, stdout, "[ERROR]")

	// Only Error should go to stderr
	assert.Contains(t, stderr, "[ERROR]")
	assert.NotContains(t, stderr, "[INFO]")
}

// ============================================================================
// Edge Cases
// ============================================================================

func TestLogInfo_EmptyMessage(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		LogInfo("")
	})

	assert.Contains(t, stdout, "[INFO]")
	// Message is empty but prefix should still be there
}

func TestLogInfo_SpecialCharacters(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		LogInfo("path: /var/log/file.txt, user: test@example.com")
	})

	assert.Contains(t, stdout, "/var/log/file.txt")
	assert.Contains(t, stdout, "test@example.com")
}

func TestLogInfo_Unicode(t *testing.T) {
	stdout, _ := captureOutput(t, func() {
		LogInfo("status: %s", "✓ complete")
	})

	assert.Contains(t, stdout, "✓ complete")
}
