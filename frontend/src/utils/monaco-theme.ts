export type MonacoThemeName = "logchef-light" | "logchef-dark";

const themeTokens = {
  "editor.background": "--card",
  "editor.foreground": "--foreground",
  "editorCursor.foreground": "--primary",
  "editor.selectionBackground": "--primary",
  "editor.lineHighlightBackground": "--muted",
  "editorLineNumber.foreground": "--muted-foreground",
  "editorWidget.background": "--popover",
  "editorWidget.foreground": "--popover-foreground",
  "editorWidget.border": "--border",
  "editorWidget.resizeBorder": "--ring",
  "editorSuggestWidget.background": "--popover",
  "editorSuggestWidget.foreground": "--popover-foreground",
  "editorSuggestWidget.border": "--border",
  "editorSuggestWidget.selectedBackground": "--accent",
  "editorSuggestWidget.selectedForeground": "--accent-foreground",
  "editorSuggestWidget.highlightForeground": "--primary",
  "editorSuggestWidget.focusHighlightForeground": "--primary",
  "editorSuggestWidget.focusOutline": "--ring",
  "editorHoverWidget.background": "--popover",
  "editorHoverWidget.foreground": "--popover-foreground",
  "editorHoverWidget.border": "--border",
  "editorHoverWidget.statusBarBackground": "--muted",
  "dropdown.background": "--popover",
  "dropdown.foreground": "--popover-foreground",
  "dropdown.border": "--border",
  "list.hoverBackground": "--accent",
  "list.highlightForeground": "--primary",
} satisfies Record<string, string>;

// Dark --primary on the focused row's --accent is 1.7:1; the bold match reads in the row's own color.
const darkThemeTokens: Partial<Record<string, string>> = {
  "editorSuggestWidget.focusHighlightForeground": "--accent-foreground",
};

const themeTokenAlpha: Partial<Record<string, number>> = {
  "editor.selectionBackground": 0.3,
};

export function rgbaToHex(rgba: Uint8ClampedArray): string {
  const channels = rgba[3] === 255 ? rgba.subarray(0, 3) : rgba;
  return `#${Array.from(channels, channel => channel.toString(16).padStart(2, "0")).join("")}`;
}

/** Reads the app's color tokens for one mode, whichever mode the page shows now. */
export function getMonacoThemeColors(mode: "light" | "dark"): Record<string, string> {
  const probe = document.createElement("div");
  probe.className = mode;
  probe.hidden = true;
  document.body.append(probe);
  const styles = getComputedStyle(probe);
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = 1;
  const context = canvas.getContext("2d", { willReadFrequently: true });
  try {
    if (!context) return {};
    return Object.fromEntries(Object.entries(themeTokens).map(([color, token]) => {
      context.clearRect(0, 0, 1, 1);
      context.globalAlpha = themeTokenAlpha[color] ?? 1;
      const modeToken = mode === "dark" ? darkThemeTokens[color] ?? token : token;
      context.fillStyle = styles.getPropertyValue(modeToken).trim();
      context.fillRect(0, 0, 1, 1);
      return [color, rgbaToHex(context.getImageData(0, 0, 1, 1).data)];
    }));
  } finally {
    probe.remove();
  }
}
