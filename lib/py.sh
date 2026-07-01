#!/usr/bin/env sh
# lib/py.sh — Python integration.
#
# The toolkit owns a venv ($MU_PY_VENV, default from defaults.env) holding its
# Python deps (rich, tqdm). `mu_py` runs helper scripts through that venv;
# `mu_py_bootstrap` creates/refreshes it. Python itself is assumed present
# (interpreter = $MU_PYTHON).

# Print the version of a python interpreter (empty on failure).
_mu_py_version() {
  "$1" -c 'import sys; print(".".join(map(str, sys.version_info[:3])))' 2> /dev/null
}

# Run a toolkit python helper via the managed venv.
mu_py() {
  if [ ! -x "${MU_PY_VENV}/bin/python" ]; then
    mu_log "ERROR" "Python venv not found at ${MU_PY_VENV}; run mu_py_bootstrap first."
    return 1
  fi
  "${MU_PY_VENV}/bin/python" "$@"
}

# mu — the mayhl_utils CLI (Typer). Management + transfer plane; needs the venv.
mu() { mu_py "${MU_ROOT}/lib/py/cli.py" "$@"; }

# Create (or refresh) the venv and install requirements.txt.
mu_py_bootstrap() {
  local py ver
  py="${MU_PYTHON:-python3}"

  if ! command -v "$py" > /dev/null 2>&1; then
    mu_log "ERROR" "Python interpreter '${py}' not found (set MU_PYTHON)."
    return 1
  fi

  ver=$(_mu_py_version "$py")
  if [ -z "$ver" ]; then
    mu_log "ERROR" "Could not determine version of '${py}'."
    return 1
  fi
  mu_log "INFO" "Bootstrapping venv at ${MU_PY_VENV} using ${py} (Python ${ver})."

  if ! "$py" -m venv "${MU_PY_VENV}"; then
    mu_log "ERROR" "venv creation failed."
    return 1
  fi

  "${MU_PY_VENV}/bin/python" -m pip install --quiet --upgrade pip &&
    "${MU_PY_VENV}/bin/python" -m pip install --quiet -r "${MU_ROOT}/requirements.txt" ||
    {
      mu_log "ERROR" "pip install failed."
      return 1
    }

  mu_log "OK" "Python venv ready at ${MU_PY_VENV} (Python ${ver})."
}
