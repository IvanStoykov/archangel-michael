#!/usr/bin/env bash
# Load tasks.manifest.yaml into bash arrays (no PyYAML).

load_tasks_manifest() {
  local manifest="${1:?manifest path required}"
  local line id prompt repo section in_task=0 in_sections=0

  if [[ ! -f "$manifest" ]]; then
    echo "ERROR: missing manifest: $manifest" >&2
    return 1
  fi

  ORDER=()
  declare -gA TASK_REPOS=()
  declare -gA TASK_PROMPT=()
  declare -gA TASK_SECTIONS=()

  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%%#*}"
    line="${line#"${line%%[![:space:]]*}"}"

    if [[ "$line" =~ ^-[[:space:]]+id:[[:space:]]+(.+)$ ]]; then
      id="${BASH_REMATCH[1]}"
      in_task=1
      in_sections=0
      continue
    fi

    [[ "$in_task" -eq 0 ]] && continue

    if [[ "$line" =~ ^prompt_file:[[:space:]]+(.+)$ ]]; then
      prompt="${BASH_REMATCH[1]}"
      continue
    fi
    if [[ "$line" =~ ^repo_path:[[:space:]]+(.+)$ ]]; then
      repo="${BASH_REMATCH[1]}"
      continue
    fi
    if [[ "$line" =~ ^expected_sections:[[:space:]]*$ ]]; then
      in_sections=1
      sections=""
      continue
    fi
    if [[ "$in_sections" -eq 1 && "$line" =~ ^-[[:space:]]+\"(.+)\"$ ]]; then
      section="${BASH_REMATCH[1]}"
      if [[ -n "$sections" ]]; then
        sections="${sections}|${section}"
      else
        sections="$section"
      fi
      continue
    fi
    if [[ "$in_sections" -eq 1 && ! "$line" =~ ^- ]]; then
      in_sections=0
    fi

    # Next task or end of sections block — commit previous task
    if [[ "$line" =~ ^-[[:space:]]+id: ]] || [[ "$line" =~ ^tasks: ]] || \
       ([[ -n "$id" && -n "$prompt" && -n "$repo" ]] && [[ "$line" == "" ]] && [[ "$in_sections" -eq 0 ]]); then
      :
    fi

    if [[ -n "$id" && -n "$prompt" && -n "$repo" && "$line" =~ ^-[[:space:]]+id: ]] && [[ "${BASH_REMATCH[1]}" != "$id" ]]; then
      ORDER+=("$prompt")
      TASK_REPOS["$prompt"]="$repo"
      TASK_PROMPT["$id"]="$prompt"
      TASK_SECTIONS["$id"]="${sections:-}"
      id="${BASH_REMATCH[1]}"
      prompt=""
      repo=""
      sections=""
      in_sections=0
      continue
    fi
  done < "$manifest"

  # Re-parse with simpler state machine (more reliable)
  ORDER=()
  TASK_REPOS=()
  TASK_PROMPT=()
  TASK_SECTIONS=()

  id=""
  prompt=""
  repo=""
  sections=""
  in_sections=0

  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%%#*}"
    line="${line#"${line%%[![:space:]]*}"}"

    if [[ "$line" =~ ^-[[:space:]]+id:[[:space:]]+(.+)$ ]]; then
      if [[ -n "$id" && -n "$prompt" && -n "$repo" ]]; then
        ORDER+=("$prompt")
        TASK_REPOS["$prompt"]="$repo"
        TASK_PROMPT["$id"]="$prompt"
        TASK_SECTIONS["$id"]="$sections"
      fi
      id="${BASH_REMATCH[1]}"
      prompt=""
      repo=""
      sections=""
      in_sections=0
      continue
    fi
    [[ -z "$id" ]] && continue

    if [[ "$line" =~ ^prompt_file:[[:space:]]+(.+)$ ]]; then
      prompt="${BASH_REMATCH[1]}"
      continue
    fi
    if [[ "$line" =~ ^repo_path:[[:space:]]+(.+)$ ]]; then
      repo="${BASH_REMATCH[1]}"
      continue
    fi
    if [[ "$line" =~ ^expected_sections:[[:space:]]*$ ]]; then
      in_sections=1
      continue
    fi
    if [[ "$in_sections" -eq 1 && "$line" =~ ^-[[:space:]]+\"(.+)\"$ ]]; then
      section="${BASH_REMATCH[1]}"
      if [[ -n "$sections" ]]; then
        sections="${sections}|${section}"
      else
        sections="$section"
      fi
    elif [[ "$in_sections" -eq 1 && -n "$line" && ! "$line" =~ ^- ]]; then
      in_sections=0
    fi
  done < "$manifest"

  if [[ -n "$id" && -n "$prompt" && -n "$repo" ]]; then
    ORDER+=("$prompt")
    TASK_REPOS["$prompt"]="$repo"
    TASK_PROMPT["$id"]="$prompt"
    TASK_SECTIONS["$id"]="$sections"
  fi

  if [[ ${#ORDER[@]} -eq 0 ]]; then
    echo "ERROR: no tasks parsed from $manifest" >&2
    return 1
  fi
}

task_id_for_prompt() {
  local pf="$1"
  local k
  for k in "${!TASK_PROMPT[@]}"; do
    if [[ "${TASK_PROMPT[$k]}" == "$pf" ]]; then
      echo "$k"
      return 0
    fi
  done
  echo "${pf%.txt}"
}
