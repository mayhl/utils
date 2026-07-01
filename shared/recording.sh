#!/usr/bin/env zsh
# shared/recording.sh — asciinema cast pipeline.  ON HOLD / WIP.
#
# NOT sourced by init.sh — parked verbatim from general.sh pending the recording
# rework (see ARCHITECTURE.md "Recording"). Known issues to resolve in the
# rework: swap_rec hardcodes a ~/.config p10k path (layering violation); swap_rec
# + the marker-based crop are workarounds to delete; the toolchain moves to
# asciinema-automation + asciinema-editor.py in a declared venv (MU_PY_VENV/cast).
# Uses zsh-only features (${1:e}/${1:r}, [[ -v ]]) — hence the zsh shebang.
#
# Kept here only so the code isn't lost; do not rely on it.

swap_rec() {

  DEMO_P10K="${HOME}/.config/oh_my_zsh/p10k_config_asciinema.zsh"

  if [[ -v _RECORD_FLAG ]]; then
    P10K_CONFIG=$_OLD_P10K_CONFIG
    source "$P10K_CONFIG"
    unset _RECORD_FLAG
    unset _OLD_P10K_CONFIG

  else
    _OLD_P10K_CONFIG="$P10K_CONFIG"
    P10K_CONFIG=$DEMO_P10K
    source "$DEMO_P10K"
    export _RECORD_FLAG="T"
  fi

}

record() {

  if [[ "${1:e}" ]]; then
    name="${1:r}"
  else
    name="$1"
  fi

  if [[ ! -v _RECORD_FLAG ]]; then
    echo "Run swap_rec first. Exiting!!!"
    return 1
  fi

  asciinema rec "${name}.cast" "${@:2}"
  asciinema convert -f asciicast-v2 "${name}.cast" "${name}.v2.cast" || return 1

  line_num=$(grep "START RECORDING" "${name}.v2.cast" -n | tail -n 1 | cut -d : -f1)
  if [[ $line_num > 0 ]]; then
    head -n 1 "${name}.v2.cast" > "${name}.v3.cast"
    sed "1,${line_num}d" "${name}.v2.cast" >> "${name}.v3.cast"
    mv -f "${name}.v3.cast" "${name}.v2.cast"
  fi

  line_num=$(grep "\^D" "${name}.v2.cast" -n | tail -n 1 | cut -d : -f1)
  if [[ $line_num > 0 ]]; then
    line_num=$((line_num - 1))
    head -n ${line_num} "${name}.v2.cast" > "${name}.cast"
    rm "${name}.v2.cast"
  else
    mv -f "${name}.v2.cast" "${name}.cast"
  fi

  cat "${name}.cast" | npx svg-term-cli --out "${name}.svg" --window
}

crop_record() {

  name=${1:r}
  orig=$1
  new="${name}.v2.cast"
  tmp="${name}.tmp.cast"

  cp "${orig}" "${new}"

  line_num=$(grep "START_RECORDING" "${new}" -n | tail -n 1 | cut -d : -f1)

  echo "line_num: $line_num"
  if [[ $line_num > 0 ]]; then
    head -n 1 "${new}" > "${tmp}"
    sed "1,${line_num}d" "${new}" >> "${tmp}"
    mv -f "${tmp}" "${new}"
  fi

}

cast2svg() {

  cat "$1" | npx svg-term-cli --out "${1:r}.svg" --window

}
