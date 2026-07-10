package claude

import (
	"fmt"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// Install verifies that claude is already available in the image. The harness
// is self-contained, so bootstrap only needs a required proof before launch.
func (a Agent) Install(rc agentsapi.RunCtx) error {
	if commandExists(record.Binary) {
		rc.Log("claude already present in image; no install step required")
		return nil
	}
	return fmt.Errorf("%s harness is self-contained but binary %q is missing from PATH", record.Name, record.Binary)
}
