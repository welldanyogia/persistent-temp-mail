import { RegisterForm } from "@/components/auth/register-form";

export const metadata = {
  title: "Register - Persistent Temp Mail",
  description: "Create a new account",
};

export default function RegisterPage() {
  return (
    <div className="flex items-center justify-center min-h-[calc(100vh-4rem)]">
      <div className="w-full max-w-md">
        <RegisterForm />
      </div>
    </div>
  );
}
