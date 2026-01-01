export default function AuthLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <div className="min-h-screen bg-muted/30">
      <div className="container mx-auto px-4 py-8">
        <div className="flex flex-col items-center justify-center space-y-6">
          <div className="flex flex-col items-center space-y-2 text-center">
            <h1 className="text-2xl font-bold tracking-tight">
              Persistent Temp Mail
            </h1>
            <p className="text-sm text-muted-foreground">
              Secure, long-term temporary email service
            </p>
          </div>
          {children}
        </div>
      </div>
    </div>
  );
}
