// Package shellassets embeds the shell library files (portability + logging helpers
// and the connectivity seam) so `mu shell-init` can emit a self-sufficient shell layer
// with NO mayhl_utils source checkout on the target (the goal for HPC boxes, which get
// only the binary + a cosmetic .config). It lives at the module root because go:embed
// can't reach up out of internal/; the .sh files stay authoritative for init.sh during
// the migration, and go:embed keeps a single source of truth (emitted == sourced).
package shellassets

import _ "embed"

//go:embed lib/compat.sh
var CompatSH string

//go:embed lib/log.sh
var LogSH string

//go:embed platform/hpc.sh
var PlatformHPCSH string

//go:embed platform/local.sh
var PlatformLocalSH string

//go:embed shared/tar.sh
var TarSH string

//go:embed shared/status.sh
var StatusSH string

//go:embed shared/utils.sh
var UtilsSH string
