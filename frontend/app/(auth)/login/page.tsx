import { LoginForm } from "@/components/auth/login-form";

export const metadata = {
  title: "Login - Persistent Temp Mail",
  description: "Sign in to your account",
};

export default function LoginPage() {
  return (
    <div className="flex items-center justify-center min-h-[calc(100vh-4rem)]">
      <div className="w-full max-w-md">
        <LoginForm />
      </div>
    </div>
  );
}
