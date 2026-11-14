# Windows tunnel adapter

The Flutter client starts a libXray worker next to the application. The worker
reads connection credentials from the parent process over stdin and configures
Wintun. The IPv4 route table includes the complement of the shared direct
networks. The Wintun adapter also carries the IPv6 default route.

Build with Go 1.27.3 or later and Flutter on Windows, using Bash for the worker
build. Obtain the official Wintun DLL for the same architecture as the worker
and Flutter runner. See [Wintun downloads](https://www.wintun.net/).

```sh
scripts/build_windows_tunnel.sh amd64 /path/to/amd64/wintun.dll
flutter build windows --dart-define=API_BASE_URL=https://26xvpn.com
```

Run 26x as administrator for this development build. Windows needs
administrative privileges to create the adapter and install routes.
Disconnect before upgrading. The worker closes the tunnel when the app
closes its stdin pipe. It reports connected after libXray starts.

On Windows, verify route installation, DNS, direct access to a destination
in the CIDR list, foreign egress through the selected node, and route cleanup
after disconnect and app exit. The Windows worker cross-builds for AMD64 and
ARM64. Native Flutter packaging and route behavior still need on-device
verification.
