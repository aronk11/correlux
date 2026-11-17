# 33. Panes follow the selection beside them, and tabs hold whole investigations

- Status: accepted
- Date: 2026-11-17
- Amends: [23](0023-consistent-sibling-navigation.md)

## Context

[ADR 23](0023-consistent-sibling-navigation.md) made the readings of one
application behave as siblings: Why, Events, Logs and the application detail
each remember where they were opened from and keep their own scroll. It also
left the wide-terminal version out, and `docs/ux-review.md` kept it as the first
follow-up. The cost it was meant to remove is still there. Reading the logs of
a pod means leaving the list the pod was picked from, and the next pod costs
`Esc`, a move, `l` and a wait. During an incident that is the whole job, done
in a loop.

Two things were wanted from the answer. The reading beside a list has to
follow the list: walking down the pods should change the logs next to them
without a keypress. And a second line of inquiry, another application or
another namespace, has to be possible without losing the first.

Three obvious routes were looked at and rejected, below.

## Decision

**A screen is a few panes, and a pane is the state of one view.** Everything
that used to live on the root model because there was only one view of it (the
view, the cursor, the scroll positions, the filter, the log stream, the object
being read) moved into a `pane`. The model embeds the focused pane, so the
code that handles a screen is the code that always handled it, now running
against whichever pane has the keyboard.

**Companions follow the selection of the pane beside them.** `|` splits to the
right and `_` below. Either asks what to open: Logs, Why, Details, YAML or
Events, which are companions, or "Another view", which is an independent pane
with its own navigation. A companion shows the selection of the first
independent pane and changes when it does. `P` pins one to what it shows now,
and again lets it follow. `Esc` in a companion closes it; `X` closes any pane.
`I` lays out the incident: why, logs and events of the application in hand,
next to the list.

A companion catches up 150 ms after the selection stops moving. Holding the
down key through forty pods must not open forty log streams against somebody's
API server ([ADR 17](0017-timed-refresh-not-watches.md)), and a pause shorter
than a glance is enough to tell walking past a row from stopping on it. Where
the selection has nothing to say to a companion, the companion says so in a
sentence rather than going blank: an empty pane reads as one still loading.

**One arrangement, not a tree.** The first pane keeps one side of the screen
and the others share the other. Side by side from 120 columns, stacked below
that. When the body cannot hold that many panes at a readable size, one pane is
drawn at a time and its title says "pane 2 of 3". `z` zooms the focused pane
and back; `Tab`, `Shift+Tab` and a mouse click move between them. A screen
holds at most four panes. A layout that always puts the list on one side and
what it explains on the other is one nobody has to arrange, and nothing has to
be remembered about how it was arranged.

**Tabs hold whole investigations.** `T` opens a tab on the dashboard, `[` and
`]` move between them, `Alt+1` to `Alt+9` jump to one, and there are at most
nine, one per digit. A tab is a set of panes with its layout and its focus. The
tab bar is drawn only from the second tab on, so a session that never opens one
looks as it did before. A tab that is not on screen stops its log streams and
opens them again when it comes back, for the reason given in ADR 17: a stream
nobody is looking at is still a connection.

**Every answer is addressed to the pane that asked.** A table, an object, a log
batch or an evidence answer comes back wrapped in a `paneMsg` carrying the
pane's number, and `Update` handles it as if that pane had the focus. An answer
for a pane that has since been closed is dropped. The first pane's messages go
out bare, so a session that never splits takes exactly the paths it took before.
Generation counters ([ADR 5](0005-explicit-async-state.md)) still discard stale
answers within a pane; addressing only decides which pane's counter to ask.

**What is cluster-wide stays shared.** The applications, the evidence, the
resource catalog and the context and namespace scope belong to the session.
All panes and all tabs look at one cluster in one scope, so switching either
moves all of them, and a diagnosis is loaded once however many panes show it.

**Refreshing is unchanged in kind.** The timed reload reloads what the panes on
screen show, and still only that.

**A saved investigation remembers its panes.** Saved investigations
([ADR 26](0026-bounded-investigation-reports.md)) keep the companions and the
split direction, so reopening one puts the logs and the diagnosis back beside the
list. What is saved is the layout, never the cluster data it showed: the panes
are filled again from the cluster. This arrives in a follow-up commit and is
recorded here because it is the same decision.

## Alternatives considered

- **Embed tmux, or ask people to run Correlux inside it.** tmux does not exist
  on Windows, and Correlux is one static binary with nothing else to install
  ([ADR 2](0002-go-and-bubble-tea.md)). Each tmux pane would be a separate
  Correlux process, and a process cannot follow the selection of another; the
  point of a companion is lost. Its prefix keys also collide with ours.
- **A free-form split tree**, where any pane can be split again in either
  direction. It is the general answer and the wrong one here: it asks the user
  to arrange a screen before looking at the problem, it needs keys to resize
  and rotate, and most arrangements cannot be read at 80 columns. One list and
  a few readings of it covers what is done in an incident.
- **One process, one cluster scope per pane.** Two namespaces or two clusters
  side by side. It would make every cached value, the catalog and the evidence
  keyed by scope, and a switch in one pane would have to be told apart from a
  switch in another. Two clusters at once is what the fleet view is for
  ([ADR 19](0019-fleet-overview.md)).

## Consequences

- The first independent pane is where something is picked; everything else is
  a reading of it. This is the same rule as ADR 23's siblings, drawn at once
  instead of one after another, and `logFrom`, `whyFrom` and `activityFrom`
  keep working inside a pane.
- A new asynchronous message has to be addressed. The commands a pane starts are
  tagged with its number on the way out, and a message type that is about the
  session rather than a pane is the exception that has to be listed. A message
  that is neither is the likeliest way to put an answer on the wrong screen,
  which is why the addressing is in one place.
- An interactive shell (`x`) still takes the whole terminal. A shell inside a
  pane needs a terminal emulator, and it is future work, not a gap hidden by
  the layout.
- A session has one context and one scope. Opening a tab does not open a second
  cluster.
- Four panes and nine tabs are limits chosen for what can be read and what has a
  key, and they are named in the notice that appears at them.
- The command palette lists every pane and tab action under "Panes" and "Tabs",
  with the key beside it, so nothing here has to be remembered.
