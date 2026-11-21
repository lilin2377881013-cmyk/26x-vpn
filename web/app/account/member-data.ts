const origin = process.env.API_ORIGIN ?? "http://127.0.0.1:8080";

export async function readAPI(path: string, token: string) {
  return fetch(new URL("/api/v1/" + path, origin), {
    cache: "no-store",
    signal: AbortSignal.timeout(5000),
    headers: { Cookie: "26x_member_session=" + encodeURIComponent(token), "X-26x-Portal": "1" },
  });
}
