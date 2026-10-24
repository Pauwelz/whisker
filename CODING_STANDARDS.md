# Coding standards

Good code is simple, clear, and purposeful.
Before you write code, apply the Ponytail ladder.

## SOLID

### Single Responsibility
Each class, module, or file has one reason to change.
- Keep UI code free of decision logic. Put that logic in a testable module.
- Keep stores and transports focused on data and I/O, not on presentation or policy.
- Split a module when it changes for unrelated reasons.

### Open/Closed
Extend behaviour with a new implementation only where a real extension point exists.
If no extension point exists, edit the code. Do not add one "for later" (see YAGNI).

### Liskov Substitution
Each implementation must be usable wherever its interface or base type is used.
It honours the full contract. It does not throw on allowed members, and it does not weaken guarantees such as ordering or disposal.

### Interface Segregation
Prefer small, focused interfaces. A caller does not depend on members it never uses.

### Dependency Inversion
High-level code depends on abstractions, and concrete implementations are injected.
Add an abstraction only when there is a second implementation or a test needs the seam.

## DRY
- Keep one source of truth per fact: a constant, a string shown to users, a path rule, a set of exit codes.
- If two places must agree, cover both with matching tests.
- Extract repeated logic into one function or module, and call it from both places.
- Do not dispatch the same commands through two paths, such as a switch and a handler table.

## KISS
- Use what the platform and existing dependencies provide before you build a custom solution.
- Names say what a thing does or holds.
- Break large methods and views into smaller pieces.
- Keep external dependencies to a minimum.

## YAGNI
Build only what the current spec or ticket asks for.
- No features, parameters, hooks, or settings "just in case".
- No configuration system until something must be configurable.
- No optimisation before a measurement shows it is needed.

## Applying these in review
A clear breach of a rule here is a finding.
Name the rule, quote the code with its file and line, and suggest the simpler shape.
