export function deviceSystemLabel(device: {
  platform: string;
  osName?: string | null;
  osVersion?: string | null;
}) {
  switch (device.platform) {
    case "ios":
      return device.osVersion
        ? `${device.osName ?? "iOS"} ${device.osVersion}`
        : "iOS · 版本未知";
    case "android": return "Android";
    case "windows": return "Windows";
    case "macos": return "macOS";
    default: return device.platform;
  }
}
