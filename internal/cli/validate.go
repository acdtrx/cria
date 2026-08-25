package cli

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"cria/internal/config"
	"cria/internal/serve"
	"cria/internal/tools"
)

// validateSynopsis is the command line every validate refusal points back at
// (docs/specs/CLI.md).
const validateSynopsis = "cria validate <id> [choice=option ...] [" + ignoreBusyFlag + "]"

// The exit codes a validation answers with, on top of the two every subcommand
// shares (cli.go). They are four outcomes an agent branches on, and nothing
// finer: the entry serves (exitOK) · it does not, and the machine is as
// validate found it (exitFailure) · validate would not begin, and touched
// nothing (exitRefused) · the machine is NOT as validate found it
// (exitUnrestored), the one outcome a person has to act on.
//
// Refusing and failing to route a command line are the same code deliberately:
// both mean cria did nothing to the host, which is the only distinction a caller
// can act on.
const (
	exitRefused    = exitUsage
	exitUnrestored = 3
)

// validate runs `cria validate <id> [choice=option ...]`: the one blocking
// command that answers whether an entry serves, on a machine that is already
// serving something else.
//
// The protocol is stop the server holding the target's port keeping its record,
// start the target, prove it with a real completion, stop it, and put the
// displaced server back from the record cria held while it was gone. The caller
// — a coding agent that just wrote the entry, on the machine that runs its own
// model — only has to infer before and after: it gets an exit code and one
// reason line, and its own branch never burns.
//
// Everything down to the first stop is a refusal that leaves the host exactly as
// it was; from there on the displaced server is put back whatever happens
// (swap).
func (a *app) validate(args []string) int {
	rest, ignoreBusy, unknown := splitFlag(args, ignoreBusyFlag)
	if unknown != "" {
		return a.usage("validate: unknown flag %s; usage: %s", unknown, validateSynopsis)
	}
	ids, explicit, err := splitPicks(rest)
	if err != nil {
		return a.usage("validate: %v; usage: %s", err, validateSynopsis)
	}
	if len(ids) == 0 {
		return a.usage("validate: no entry named; usage: %s", validateSynopsis)
	}
	if len(ids) > 1 {
		return a.usage("validate: one entry at a time (got %s); usage: %s",
			strings.Join(ids, ", "), validateSynopsis)
	}
	id := ids[0]

	// The tree is read once, before anything is stopped, and held for the whole
	// protocol: putting the displaced server back resolves its record against the
	// tree (serve.Replay), and that resolution must not depend on a config file
	// still reading the same way minutes later.
	tree, err := a.tree()
	if err != nil {
		return a.failWith(exitRefused, "validate %s: %v", id, err)
	}
	entry, found := entryNamed(tree, id)
	if !found {
		return a.failWith(exitRefused, "validate %s: %s", id, unknownEntry(tree, id))
	}
	selection, err := a.launchPicks(entry, explicit)
	if err != nil {
		return a.failWith(exitRefused, "validate %s: %v", id, err)
	}
	// Resolution ahead of every gate, as a start does it (docs/specs/SERVE.md,
	// Start 1): a pick that names nothing must be answered with the valid names,
	// never with a stopped server behind it.
	if _, err := config.Resolve(entry, selection); err != nil {
		return a.failWith(exitRefused, "validate %s: %v", id, err)
	}

	manager, err := a.servers()
	if err != nil {
		return a.failWith(exitRefused, "validate %s: %v", id, err)
	}
	report := a.tools(tree.Settings)
	if _, err := serve.LaunchTool(entry.Backend, report); err != nil {
		return a.failWith(exitRefused, "validate %s: %v", id, err)
	}

	displaced, err := manager.Displaced(entry)
	if err != nil {
		return a.failWith(exitRefused, "validate %s: %v", id, err)
	}
	if refusal := a.displacementRefusal(manager, entry, displaced, ignoreBusy); refusal != "" {
		return a.failWith(exitRefused, "validate %s: %s", id, refusal)
	}
	return a.swap(manager, tree, entry, selection, report, displaced)
}

// displacementRefusal is why the port a validation needs cannot be taken over,
// or the empty string when it can. It is the last thing asked before the machine
// changes, and the warning it may print on the way is part of the same
// judgement: what cria could not check about the server it is about to stop.
//
// ignoreBusy is the operator's word that a generation cut off mid-answer is
// acceptable, and it lifts that one gate. The other two refusals stand under it,
// because neither is about anybody's patience: a process cria did not start has
// no record to put it back from, and a target already serving elsewhere would be
// orphaned by its own second start.
func (a *app) displacementRefusal(manager servers, entry config.Entry, displaced serve.Displacement, ignoreBusy bool) string {
	if len(displaced.Foreign) > 0 {
		return foreignRefusal(entry, displaced.Foreign)
	}

	// A target already running somewhere other than the port it declares now is
	// refused rather than displaced: starting it again would write over its own
	// record and leave the first process running with nothing naming it. A target
	// running on that port *is* the holder, and is displaced like any other
	// server — self-validation is the same protocol, not a special case.
	held, running, err := manager.Running(entry.ID)
	if err != nil {
		return err.Error()
	}
	if running && (displaced.Holder == nil || displaced.Holder.EntryID != entry.ID) {
		return fmt.Sprintf("%s is already running as pid %d on port %d, which is not the port it launches on now (%d); validating would leave that process with nothing naming it — `cria stop %s` first",
			held.EntryID, held.PID, held.Port, entry.Port, held.EntryID)
	}

	if displaced.Holder == nil {
		return ""
	}
	refusal, warning := a.busyGate(manager, *displaced.Holder, ignoreBusy)
	if warning != "" {
		a.note("%s", warning)
	}
	return refusal
}

// busyGate is what the port holder's own work says about stopping it right now:
// the refusal that keeps a generation from being cut off mid-answer, or the
// warning a swap goes ahead under when cria could not tell.
//
// A busy holder is refused rather than waited out. Queueing would burn the
// caller's own clock invisibly — the caller is a coding agent whose turn is
// running while cria blocks — so the honest answer is the refusal, and the
// action it names is a human's: let the answer finish, or stop the server.
// ignoreBusy is that human having answered already, so the verdict is still read
// and reported, and the swap goes ahead over it.
//
// Unverifiable is neither idle nor busy: it is the case where nothing on the
// machine can answer the question (an mlx server, a llama build without the slot
// endpoint), and the swap proceeds with the risk named rather than refusing —
// the machine that needs validate runs one server on one port, usually the
// caller's own, idle at the moment the tool call executes.
//
// The refusal does not name the override. The agent reading it is the one whose
// request would be cut off, and a bypass on the line it reads is a bypass it
// will take; the flag is documented where the person deciding reads
// (`cria --help`).
func (a *app) busyGate(manager servers, holder serve.Record, ignoreBusy bool) (refusal, warning string) {
	generation := manager.Generating(holder)
	switch generation.Busy {
	case serve.BusyGenerating:
		if ignoreBusy {
			return "", fmt.Sprintf("%s is answering a request on port %d right now; %s was given, so cria stops it mid-answer",
				holder.EntryID, holder.Port, ignoreBusyFlag)
		}
		return fmt.Sprintf("%s is answering a request on port %d right now, and stopping it would cut that answer off; ask the user to let it finish or to stop %s, then validate again",
			holder.EntryID, holder.Port, holder.EntryID), ""
	case serve.BusyUnverifiable:
		return "", fmt.Sprintf("cria cannot tell whether %s is generating right now (%s); validating stops it anyway, so a request in flight would die with it",
			holder.EntryID, generation.Detail)
	}
	return "", ""
}

// swap is the protocol from the first thing it changes: the holder is stopped
// with its record held, the target is put through start-serve-prove-stop, and
// the holder goes back where it was.
//
// Restore is unconditional: whatever the target's verdict, and whether or not
// the operator interrupted, the displaced server is put back before cria says
// anything about the entry it was asked about. The one thing that can stand in
// the way is a target that would not stop — its port is still taken, so putting
// the holder back would spawn a second server onto a port nothing can bind, and
// that is reported as the unrestored machine it is.
func (a *app) swap(manager servers, tree *config.Tree, entry config.Entry, selection config.Selection, report tools.Report, displaced serve.Displacement) int {
	// The Ctrl-C watch covers the protocol and nothing else. A swap abandoned
	// halfway is the one state validate must not leave behind, so an interrupt
	// ends the stages rather than the process, the restore still runs, and the
	// exit says what the machine was left as. Outside this, Ctrl-C ends cria the
	// way it always does.
	interrupted, disarm := a.interrupts()
	defer disarm()

	var held serve.Record
	holding := false
	if holder := displaced.Holder; holder != nil {
		a.printf("stopping %s (holding its record)\n", holder.EntryID)
		// The record travels as a value: the stop removes the file it was read
		// from, and this copy is the only thing that can put the server back.
		stopped, err := manager.Displace(*holder)
		if err != nil {
			return a.failWith(exitUnrestored, "validate %s: cannot stop %s on port %d: %v; nothing was validated, and %s may already be down — `cria status` says whether it is still serving",
				entry.ID, holder.EntryID, holder.Port, err, holder.EntryID)
		}
		held, holding = stopped, true
	}

	record, failure := a.runTarget(manager, entry, selection, report, interrupted)

	if record != nil {
		a.printf("stopping %s\n", record.EntryID)
		if err := manager.Stop(*record); err != nil {
			return a.failWith(exitUnrestored, "validate %s: started it but cannot stop it again: %v; %s",
				entry.ID, err, unrestorable(*record, holding, held))
		}
	}

	if holding {
		a.printf("restoring %s…\n", held.EntryID)
		restored, err := manager.Restore(tree, held, report)
		if err != nil {
			return a.failWith(exitUnrestored, "validate %s: %v; nothing is serving on port %d now — `cria start %s` once that is fixed",
				entry.ID, err, held.Port, held.EntryID)
		}
		a.printf("restored %s as pid %d on %s\n", restored.EntryID, restored.PID, address(restored))
	}

	if failure != nil {
		return a.failWith(exitFailure, "validate %s: %v", entry.ID, failure)
	}
	a.printf("validated %s: it served on port %d and answered a completion\n", entry.ID, displaced.Port)
	return exitOK
}

// runTarget puts the target through the whole of what validation means: start
// it, wait until it serves, and ask it for one real completion — the answer no
// health signal stands in for (serve.Prove).
//
// It answers the record it started, so the caller can stop what it left running,
// and why the target is not validated. A nil record means the start itself never
// happened and there is nothing to stop; a nil error means the entry serves.
func (a *app) runTarget(manager servers, entry config.Entry, selection config.Selection, report tools.Report, interrupted func() bool) (*serve.Record, error) {
	if interrupted() {
		return nil, errors.New("interrupted before it was started")
	}

	a.printf("starting %s…\n", entry.ID)
	record, err := manager.Start(entry, selection, report)
	if err != nil {
		return nil, err
	}
	if _, _, err := a.awaitGreen(manager, record, interrupted); err != nil {
		return &record, err
	}
	if interrupted() {
		return &record, errors.New("interrupted before it was proved")
	}

	a.printf("proving %s…\n", record.EntryID)
	if err := manager.Prove(record); err != nil {
		return &record, err
	}
	return &record, nil
}

// unrestorable says what a target that would not stop leaves behind: the port it
// is still holding, the server that therefore cannot go back onto it, and the
// two commands a person runs to end up where validate promised.
func unrestorable(target serve.Record, holding bool, held serve.Record) string {
	if !holding {
		return fmt.Sprintf("%s is still serving on port %d; `cria stop %s` ends it", target.EntryID, target.Port, target.EntryID)
	}
	return fmt.Sprintf("%s still holds port %d and %s was not put back; `cria stop %s`, then `cria start %s`",
		target.EntryID, target.Port, held.EntryID, target.EntryID, held.EntryID)
}

// watchInterrupt arms the Ctrl-C watch a swap runs under and hands back the two
// halves of it: whether the operator has asked cria to stop, and the disarm that
// gives the signal back to the runtime.
//
// The signal is latched rather than acted on. There is no safe moment to end a
// process partway through a swap, so what an interrupt does is make the next
// stage boundary the last one: the target is abandoned, the displaced server
// goes back, and cria exits saying so. A stage already in flight — a spawn, a
// completion — is bound by its own budget, because the watch is read between
// them and never inside one.
func watchInterrupt() (interrupted func() bool, disarm func()) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

	// asked is read and written only by the returned check, which the protocol
	// calls from its own goroutine; the runtime's signal goroutine touches the
	// channel alone.
	asked := false
	return func() bool {
			select {
			case <-signals:
				asked = true
			default:
			}
			return asked
		}, func() {
			signal.Stop(signals)
		}
}

// uninterrupted is the watch a command that does not catch Ctrl-C runs under:
// the answer is always no, because the signal ends the process before anything
// could ask. `cria start --wait` is one — a killed start leaves its server
// running, which is exactly the state it is meant to leave behind.
func uninterrupted() bool { return false }
