import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";
import { unwrapHashlineHeaderPath } from "@oh-my-pi/pi-coding-agent";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { basename, extname, isAbsolute, join, relative, resolve } from "node:path";

const LINT_TIMEOUT_MS = 20_000;
const MAX_LISTED = 20;
const PROSE_EXTENSIONS: Record<string, true> = { ".md": true, ".txt": true };
const VIOLATION_RE = /^#\d+ (.+?):(\d+):\d+\S* - (.+)$/;
// duplicated-block names its first occurrence as file:line:col, which moves with every edit
const LOCATION_RE = /\S+:\d+(?::\d+(?:[-.]\d+)*)?/g;

type LintCommand = "code_lint" | "prose_lint";

interface Violation {
    line: number;
    key: string;
    text: string;
}

interface FileChange {
    path: string;
    before: string | null;
}

interface LintOutcome {
    command: LintCommand;
    added: Violation[];
    total: number;
}

interface CommandReport {
    added: Violation[];
    preexisting: number;
}

async function readText(path: string): Promise<string | null> {
    try {
        return await readFile(path, "utf8");
    } catch {
        return null;
    }
}

function parseViolations(stdout: string): Violation[] {
    const violations: Violation[] = [];
    for (const raw of stdout.split("\n")) {
        const match = VIOLATION_RE.exec(raw);
        if (!match) continue;
        violations.push({
            line: Number(match[2]),
            key: match[3].replace(LOCATION_RE, "<loc>"),
            text: raw.replace(/^#\d+ /, ""),
        });
    }
    return violations;
}

function changesFromEdit(details: unknown): FileChange[] {
    if (!details || typeof details !== "object") return [];
    const record = details as { perFileResults?: unknown[] };
    const entries = Array.isArray(record.perFileResults) ? record.perFileResults : [details];
    const changes: FileChange[] = [];
    for (const entry of entries) {
        const { path, op, oldText } = entry as { path?: unknown; op?: unknown; oldText?: unknown };
        if (typeof path !== "string" || op === "delete") continue;
        changes.push({ path, before: typeof oldText === "string" ? oldText : null });
    }
    return changes;
}

function formatReport(command: LintCommand, report: CommandReport): string {
    const listed = report.added.slice(0, MAX_LISTED).map((v, i) => `#${i + 1} ${v.text}`);
    if (report.added.length > MAX_LISTED) listed.push(`... ${report.added.length - MAX_LISTED} more`);
    const note = report.preexisting > 0 ? `\n(${report.preexisting} older violations in these files are not listed.)` : "";
    return `myoo ${command}: this change introduced ${report.added.length} violation(s). Fix them before moving on:\n${listed.join("\n")}${note}`;
}

class Linter {
    available = true;
    // extensions that code_lint reported as unsupported are not linted again
    readonly #unsupported = new Set<string>();

    constructor(readonly pi: ExtensionAPI) {}

    async lint(command: LintCommand, file: string, cwd: string): Promise<Violation[] | null> {
        if (!this.available || this.#unsupported.has(extname(file))) return null;
        const result = await this.pi.exec("myoo", [command, "-nf", file], { cwd, timeout: LINT_TIMEOUT_MS }).catch((error: unknown) => {
            this.available = false;
            this.pi.logger.warn("myoo lint hook disabled, myoo did not start", { error: String(error) });
            return null;
        });
        if (!result || (result.code !== 0 && result.code !== 1)) return null;
        if (`${result.stdout}${result.stderr}`.includes("unsupported file type")) {
            this.#unsupported.add(extname(file));
            return null;
        }
        return parseViolations(result.stdout);
    }

    // Lints the old content from a scratch copy that keeps the file name, so name-based rules still apply.
    async lintBefore(command: LintCommand, content: string, path: string): Promise<Violation[]> {
        const dir = await mkdtemp(join(tmpdir(), "myoo-lint-"));
        try {
            const copy = join(dir, basename(path));
            await writeFile(copy, content);
            return (await this.lint(command, copy, dir)) ?? [];
        } finally {
            await rm(dir, { recursive: true, force: true });
        }
    }

    async introduced(change: FileChange, cwd: string): Promise<LintOutcome> {
        const command: LintCommand = PROSE_EXTENSIONS[extname(change.path).toLowerCase()] ? "prose_lint" : "code_lint";
        const shown = relative(cwd, change.path);
        const target = shown.startsWith("..") || isAbsolute(shown) ? change.path : shown;
        const after = await readText(change.path);
        const violations = after === null ? null : await this.lint(command, target, cwd);
        if (!violations?.length || after === null) return { command, added: [], total: 0 };
        if (change.before === null) return { command, added: violations, total: violations.length };

        const oldLines = new Set(change.before.split("\n").map((line) => line.trim()));
        const newLines = after.split("\n");
        const added: Violation[] = [];
        const onOldLines: Violation[] = [];
        for (const v of violations) {
            (oldLines.has((newLines[v.line - 1] ?? "").trim()) ? onOldLines : added).push(v);
        }
        if (onOldLines.length > 0) {
            // Rules like long-function point at the signature line, which the edit may not touch.
            // Such a finding counts as new when the old content did not produce it.
            const knownKeys = new Set((await this.lintBefore(command, change.before, change.path)).map((v) => v.key));
            added.push(...onOldLines.filter((v) => !knownKeys.has(v.key)));
        }
        added.sort((a, b) => a.line - b.line);
        return { command, added, total: violations.length };
    }
}

export default function myooLint(pi: ExtensionAPI) {
    const linter = new Linter(pi);
    // the write tool reports no old content, so the target is read before it runs (null for a new file)
    const writeSnapshots = new Map<string, string | null>();

    pi.on("tool_call", async (event, ctx) => {
        if (event.toolName !== "write" || typeof event.input.path !== "string" || !linter.available) return;
        const path = resolve(ctx.cwd, unwrapHashlineHeaderPath(event.input.path));
        writeSnapshots.set(event.toolCallId, await readText(path));
    });

    pi.on("tool_result", async (event, ctx) => {
        const snapshot = writeSnapshots.get(event.toolCallId) ?? null;
        writeSnapshots.delete(event.toolCallId);
        if (event.isError || !linter.available) return;

        let changes: FileChange[] = [];
        if (event.toolName === "edit") changes = changesFromEdit(event.details);
        if (event.toolName === "write") {
            const resolvedPath = (event.details as { resolvedPath?: unknown } | undefined)?.resolvedPath;
            if (typeof resolvedPath === "string") changes = [{ path: resolvedPath, before: snapshot }];
        }

        const reports = new Map<LintCommand, CommandReport>();
        for (const change of changes) {
            const outcome = await linter.introduced(change, ctx.cwd);
            const report = reports.get(outcome.command) ?? { added: [], preexisting: 0 };
            report.added.push(...outcome.added);
            report.preexisting += outcome.total - outcome.added.length;
            reports.set(outcome.command, report);
        }
        const texts = [...reports].filter(([, report]) => report.added.length > 0).map(([command, report]) => formatReport(command, report));
        if (texts.length === 0) return;
        return { content: [...event.content, { type: "text", text: texts.join("\n\n") }] };
    });
}
