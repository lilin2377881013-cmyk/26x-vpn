"""Apply the 26x download shaper to the pinned node source."""
from pathlib import Path
import shutil
import sys

root = Path(sys.argv[1])
dispatcher = root / "app/dispatcher"
path = dispatcher / "default.go"
source = path.read_text()
assert "bandwidthWriter" not in source
marker = "\n\treturn inboundLink, outboundLink"
assert source.count(marker) == 1
source = source.replace(marker, """
	if user != nil && user.Email != "" {
		outboundLink.Writer = &bandwidthWriter{ctx: ctx, label: user.Email, writer: outboundLink.Writer}
	}
""" + marker, 1)
marker = "\toutbound = WrapLink(ctx, d.policy, d.stats, outbound)"
assert source.count(marker) == 1
source = source.replace(marker, marker + """
	if inbound := session.InboundFromContext(ctx); inbound != nil && inbound.User != nil && inbound.User.Email != "" {
		outbound.Writer = &bandwidthWriter{ctx: ctx, label: inbound.User.Email, writer: outbound.Writer}
	}""", 1)
path.write_text(source)
shutil.copyfile(Path(__file__).with_name("account-bandwidth.go"), dispatcher / "account_bandwidth.go")
