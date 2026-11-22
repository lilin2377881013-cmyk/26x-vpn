import type { NextRequest } from "next/server";

export const dynamic = "force-dynamic";

export async function GET(request: NextRequest) {
  const code = request.nextUrl.searchParams.get("code") ?? "";
  if (!/^[A-Za-z0-9_-]{16}$/.test(code)) return new Response(null, { status: 400 });
  const origin = process.env.API_ORIGIN ?? "http://127.0.0.1:8080";
  try {
    const session = await fetch(new URL("/api/v1/admin/me", origin), {
      headers: { cookie: request.headers.get("cookie") ?? "" },
      cache: "no-store", redirect: "error", signal: AbortSignal.timeout(4000),
    });
    if (!session.ok) return new Response(null, { status: session.status });
    const { role } = await session.json();
    if (role !== "owner" && role !== "operations") return new Response(null, { status: 403 });
    const stats = await fetch(`http://127.0.0.1:8796/referrals/${code}`, {
      cache: "no-store", redirect: "error", signal: AbortSignal.timeout(4000),
    });
    if (!stats.ok) return new Response(null, { status: 503 });
    return Response.json(await stats.json(), { headers: { "Cache-Control": "private, no-store" } });
  } catch {
    return new Response(null, { status: 503 });
  }
}
