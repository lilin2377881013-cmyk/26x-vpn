import { LoginForm } from "./login-form";

export default async function LoginPage({ searchParams }: { searchParams: Promise<{ plan?: string }> }) {
  const { plan } = await searchParams;
  return <LoginForm plan={typeof plan === "string" ? plan.slice(0, 80) : ""} />;
}
