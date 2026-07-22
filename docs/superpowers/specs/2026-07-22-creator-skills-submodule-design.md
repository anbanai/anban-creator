# Creator Skills Submodule Design

## Goal

Publish the current `plugins/` working tree as the first commit of
`https://github.com/anbanai/creator-skills.git`, then replace the directory in
the Anban Writer repository with a managed Git submodule at the same path.

## History Boundary

The new repository starts from a fresh snapshot. Historical commits from the
Anban Writer repository and the earlier plugin repositories are not migrated.
The initial snapshot includes every currently tracked file under `plugins/`
and all current uncommitted changes under that directory.

## Publication Flow

1. Copy the current `plugins/` working tree into an isolated temporary
   directory without parent-repository Git metadata.
2. Initialize a new repository with `main` as its branch and create one initial
   commit containing the complete snapshot.
3. Push that commit to `anbanai/creator-skills.git` and verify the remote
   `refs/heads/main` SHA before modifying the parent repository.
4. Preserve a temporary backup of the original directory, remove the ordinary
   tracked files from the parent index, and add `plugins/` as a submodule pinned
   to the verified child commit.

## Parent Repository Contract

The `.gitmodules` entry uses:

- path: `plugins`
- URL: `https://github.com/anbanai/creator-skills.git`
- branch: `main`
- `syncPush = true`
- `syncRemote = origin`

The sync settings make `plugins/` participate in the existing managed
submodule pre-push workflow. The parent repository records only the gitlink and
`.gitmodules`; unrelated working-tree changes remain untouched.

## Failure Handling

No parent-repository conversion occurs unless the child push succeeds and the
remote SHA matches the local child commit. The original directory backup is
retained until file-content comparison, submodule status, and remote SHA checks
all pass. A failure before parent conversion leaves the original workspace
unchanged; a failure during conversion can be recovered from the backup and the
unchanged child repository.

## Verification

- Compare the child repository file manifest and content with the pre-migration
  `plugins/` snapshot.
- Confirm `git ls-tree HEAD plugins` will record mode `160000` after the parent
  conversion is committed.
- Confirm `.gitmodules` exposes the expected URL, branch, and managed-push
  settings.
- Confirm `git submodule status plugins` reports the same SHA as remote `main`.
- Run plugin contract tests and the full Go test suite required by `AGENTS.md`.
