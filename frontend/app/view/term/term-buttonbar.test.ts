import { describe, expect, it } from "vitest";

import {
    DEFAULT_TERM_BUTTON_BAR_BUTTONS,
    getNextTermButtonBarConfig,
    isTermButtonBarVisible,
    normalizeTermButtonBarConfig,
    resolveTermButtonCommand,
} from "./term-buttonbar";

describe("button bar visibility toggle helpers", () => {
    it("provides default buttons when enabling an empty button bar", () => {
        expect(isTermButtonBarVisible(null)).toBe(false);
        expect(getNextTermButtonBarConfig(null)).toEqual(DEFAULT_TERM_BUTTON_BAR_BUTTONS);
    });

    it("returns an empty override when disabling a visible button bar", () => {
        const current = [{ label: "Status", command: "git status", addnewline: true }];

        expect(isTermButtonBarVisible(current)).toBe(true);
        expect(getNextTermButtonBarConfig(current)).toEqual([]);
    });
});
describe("normalizeTermButtonBarConfig", () => {
    it("keeps only buttons with a non-empty label and command", () => {
        expect(
            normalizeTermButtonBarConfig([
                { label: "List", command: "ls -la", addnewline: true },
                { label: "", command: "pwd" },
                { label: "No command" },
                { label: "Git", command: "git status\n" },
            ])
        ).toEqual([
            { label: "List", command: "ls -la", addnewline: true },
            { label: "Git", command: "git status\n" },
        ]);
    });

    it("treats missing or non-array config as an empty button bar", () => {
        expect(normalizeTermButtonBarConfig(null)).toEqual([]);
        expect(normalizeTermButtonBarConfig({ label: "List", command: "ls" })).toEqual([]);
    });
});

describe("resolveTermButtonCommand", () => {
    it("appends one newline when addnewline is enabled", () => {
        expect(resolveTermButtonCommand({ label: "List", command: "ls -la", addnewline: true })).toBe("ls -la\n");
        expect(resolveTermButtonCommand({ label: "Git", command: "git status\n", addnewline: true })).toBe(
            "git status\n"
        );
    });

    it("sends the configured command unchanged by default", () => {
        expect(resolveTermButtonCommand({ label: "Ctrl-C", command: "\u0003" })).toBe("\u0003");
    });
});
