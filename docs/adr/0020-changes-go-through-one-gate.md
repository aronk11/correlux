# 20. Every change goes through one gate, and kubeui never edits for you

- Status: accepted
- Date: 2026-09-02

## Context

kubeui can now change a cluster: a replica count, a whole document. That crosses
the line the product had stayed behind until this point, and the way it crosses
it decides whether people trust the tool during an incident.

Two questions had to be answered.

**How is consent asked for?** The failure mode this guards against is not a
mistyped number; it is a correct action performed on the wrong cluster. Everyone
who has run `kubectl` for a year has done it. A confirmation that says "apply
changes? [y/N]" does nothing about that, because the thing the user got wrong is
not in the question.

**Who provides the editor?** A terminal UI that edits YAML has to implement one:
cursor movement, indentation, search, undo. Every one of those is a decision
somebody has already made in their own editor, and the worst possible time to
discover kubeui's version is while changing a production Deployment.

## Decision

**One gate.** Every change — scale and edit today, delete and restart later — is
built as a `pendingAction` and shown on the same confirmation screen. That
screen states:

- what the change *does*, in consequences rather than operations: "this removes
  2 replicas", "this stops every replica: the workload will serve nothing",
  "this replaces 3 lines with 1", with the diff underneath for an edit;
- the object it will touch;
- **the cluster**, marked when it is production.

In a context classified as production ([ADR 8](0008-production-safety.md)),
Enter is not enough: the cluster's name has to be typed. It is the one word that
proves the user knows where they are. The guard is configurable, because a team
that has decided otherwise should not have to fight their tools.

Consequences are shown *before* the commitment, not after: the replica prompt
updates its note as the number is typed, so "this removes 2 replicas" is on
screen while the user still has a finger over Enter.

**No editor.** `e` writes the document to a temporary file and hands the terminal
to `$KUBE_EDITOR`, `$EDITOR` or `vi`, exactly as `kubectl edit` does. kubeui
takes the terminal back when the editor exits, compares what came back, and
shows the difference. The file is removed either way: a Kubernetes object
routinely carries secrets, and leaving one in the temp directory is not
something a tool should do quietly.

Four edits are refused before they are sent, because none of them is what
somebody meant by editing the object on screen: a document that is not valid
YAML, one with a duplicate key — the lenient reading silently keeps the last one
and loses half an edit without a word — one that renames the object, and one
that moves it to another namespace.

An edit carries the `resourceVersion` it was read at, so a document written
against a version somebody else has already replaced is refused by the server
rather than quietly overwriting them.

## Consequences

- Every future mutating action inherits the gate by construction: it is a
  `pendingAction` whose lines say what will happen, or it does not ship.
- Two keystrokes to change a replica count, three in production. That is
  deliberate friction, and it is smaller than the friction of the outage it
  prevents.
- kubeui inherits every editor's strengths and none of their bugs. It also
  inherits one failure mode: an `$EDITOR` that does not block — a GUI editor
  without `--wait` — returns immediately and kubeui sees no change. The message
  then says nothing changed, which is true and mildly annoying.
- A conflict is reported, not merged. kubeui does not attempt a three-way merge:
  telling somebody their edit was written against an old version is honest, and
  a merge that silently resolves a conflict is how two people's changes become
  one person's change.
