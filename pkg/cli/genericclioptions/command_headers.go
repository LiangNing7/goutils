package genericclioptions

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

const (
	kubectlCommandHeader = "Kubectl-Command"
	kubectlSessionHeader = "Kubectl-Session"
)

// CommandHeaderRoundTripper adds a layer around the standard
// round tripper to add Request headers before delegation. Implements
// the go standard library "http.RoundTripper" interface.
type CommandHeaderRoundTripper struct {
	Delegate http.RoundTripper
	Headers  map[string]string
}

// CommandHeaderRoundTripper adds Request headers before delegating to standard
// round tripper. These headers are kubectl command headers which
// detail the kubectl command. See SIG CLI KEP 859:
//
//	https://github.com/kubernetes/enhancements/tree/master/keps/sig-cli/859-kubectl-headers
func (c *CommandHeaderRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for header, value := range c.Headers {
		req.Header.Set(header, value)
	}
	return c.Delegate.RoundTrip(req)
}

// ParseCommandHeaders fills in a map of custom headers into the CommandHeaderRoundTripper. These
// headers are then filled into each request. For details on the custom headers see:
//
//	https://github.com/kubernetes/enhancements/tree/master/keps/sig-cli/859-kubectl-headers
//
// Each call overwrites the previously parsed command headers (not additive).
// TODO(seans3): Parse/add flags removing PII from flag values.
func (c *CommandHeaderRoundTripper) ParseCommandHeaders(cmd *cobra.Command, args []string) {
	if cmd == nil {
		return
	}
	// Overwrites previously parsed command headers (headers not additive).
	c.Headers = map[string]string{}
	// Session identifier to aggregate multiple Requests from single kubectl command.
	uid := uuid.New().String()
	c.Headers[kubectlSessionHeader] = uid
	// Iterate up the hierarchy of commands from the leaf command to create
	// the full command string. Example: kubectl create secret generic
	cmdStrs := []string{}
	for cmd.HasParent() {
		parent := cmd.Parent()
		currName := strings.TrimSpace(cmd.Name())
		cmdStrs = append([]string{currName}, cmdStrs...)
		cmd = parent
	}
	currName := strings.TrimSpace(cmd.Name())
	cmdStrs = append([]string{currName}, cmdStrs...)
	if len(cmdStrs) > 0 {
		c.Headers[kubectlCommandHeader] = strings.Join(cmdStrs, " ")
	}
}

// CancelRequest is propagated to the Delegate RoundTripper within
// if the wrapped RoundTripper implements this function.
func (c *CommandHeaderRoundTripper) CancelRequest(req *http.Request) {
	type canceler interface {
		CancelRequest(*http.Request)
	}
	// If possible, call "CancelRequest" on the wrapped Delegate RoundTripper.
	if cr, ok := c.Delegate.(canceler); ok {
		cr.CancelRequest(req)
	}
}
