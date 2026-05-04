import { describe, expect, it, vi } from "vitest";

vi.mock("electron", () => {
    class BrowserWindow {
        webContents = { send: vi.fn(), reloadIgnoringCache: vi.fn(), toggleDevTools: vi.fn() };
    }
    class BaseWindow {}
    class MenuItem {
        constructor(public template: any) {
            Object.assign(this, template);
        }
    }
    return {
        BrowserWindow,
        BaseWindow,
        MenuItem,
        Menu: {
            buildFromTemplate: vi.fn((template) => ({ template, popup: vi.fn() })),
            setApplicationMenu: vi.fn(),
        },
        ipcMain: { on: vi.fn() },
        app: { dock: { setMenu: vi.fn() } },
    };
});

vi.mock("@/app/store/wps", () => ({ waveEventSubscribeSingle: vi.fn() }));
vi.mock("@/app/store/wshclientapi", () => ({
    RpcApi: {
        GetFullConfigCommand: vi.fn(),
        WorkspaceListCommand: vi.fn(async () => []),
        SetConfigCommand: vi.fn(),
        GetMetaCommand: vi.fn(),
        SetMetaCommand: vi.fn(),
    },
}));
vi.mock("../frontend/util/util", () => ({ fireAndForget: (fn: any) => fn() }));
vi.mock("./emain-builder", () => ({ focusedBuilderWindow: null, getBuilderWindowById: vi.fn() }));
vi.mock("./emain-ipc", () => ({ openBuilderWindow: vi.fn() }));
vi.mock("./emain-platform", () => ({ isDev: false, unamePlatform: "darwin" }));
vi.mock("./emain-tabview", () => ({ clearTabCache: vi.fn() }));
vi.mock("./emain-util", () => ({
    decreaseZoomLevel: vi.fn(),
    increaseZoomLevel: vi.fn(),
    resetZoomLevel: vi.fn(),
}));
vi.mock("./emain-window", () => ({
    WaveBrowserWindow: class {},
    createNewWaveWindow: vi.fn(),
    createWorkspace: vi.fn(),
    focusedWaveWindow: null,
    getAllWaveWindows: vi.fn(() => []),
    getWaveWindowByWorkspaceId: vi.fn(),
    relaunchBrowserWindows: vi.fn(),
}));
vi.mock("./emain-wsh", () => ({ ElectronWshClient: {} }));
vi.mock("./updater", () => ({ updater: { checkForUpdates: vi.fn() } }));

describe("makeTermButtonBarViewMenuItem", () => {
    it("adds a View menu item that tells the focused renderer to toggle the terminal button bar", async () => {
        const { makeTermButtonBarViewMenuItem } = await import("./emain-menu");
        const send = vi.fn();
        const item = makeTermButtonBarViewMenuItem({ send } as any);

        expect(item.label).toBe("Toggle Terminal Button Bar");
        item.click?.(null as any, null as any, null as any);

        expect(send).toHaveBeenCalledWith("menu-item-toggle-term-button-bar");
    });
});
