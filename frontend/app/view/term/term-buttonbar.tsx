// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import "./term.scss";

export type TermButtonBarButton = {
    label: string;
    command: string;
    tooltip?: string;
    addnewline?: boolean;
};

function isRecord(value: unknown): value is Record<string, unknown> {
    return value != null && typeof value === "object" && !Array.isArray(value);
}

export function normalizeTermButtonBarConfig(value: unknown): TermButtonBarButton[] {
    if (!Array.isArray(value)) {
        return [];
    }
    const buttons: TermButtonBarButton[] = [];
    for (const rawButton of value) {
        if (!isRecord(rawButton)) {
            continue;
        }
        const label = rawButton.label;
        const command = rawButton.command;
        if (typeof label !== "string" || label.trim() === "") {
            continue;
        }
        if (typeof command !== "string" || command === "") {
            continue;
        }
        const button: TermButtonBarButton = {
            label: label.trim(),
            command,
        };
        if (typeof rawButton.tooltip === "string" && rawButton.tooltip !== "") {
            button.tooltip = rawButton.tooltip;
        }
        if (rawButton.addnewline === true) {
            button.addnewline = true;
        }
        buttons.push(button);
    }
    return buttons;
}

export function resolveTermButtonCommand(button: TermButtonBarButton): string {
    if (!button.addnewline || button.command.endsWith("\n") || button.command.endsWith("\r")) {
        return button.command;
    }
    return button.command + "\n";
}

export function TermButtonBar({
    config,
    onSendInput,
    onRequestFocus,
}: {
    config: unknown;
    onSendInput: (data: string) => void;
    onRequestFocus?: () => void;
}) {
    const buttons = normalizeTermButtonBarConfig(config);
    if (buttons.length === 0) {
        return null;
    }
    return (
        <div className="term-buttonbar" role="toolbar" aria-label="Terminal button bar">
            {buttons.map((button, index) => (
                <button
                    key={`${button.label}-${index}`}
                    className="term-buttonbar-button"
                    title={button.tooltip ?? button.command}
                    type="button"
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => {
                        onSendInput(resolveTermButtonCommand(button));
                        onRequestFocus?.();
                    }}
                >
                    {button.label}
                </button>
            ))}
        </div>
    );
}
