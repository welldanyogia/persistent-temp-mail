export function Footer() {
  return (
    <footer className="border-t bg-muted/20 py-6 px-6">
      <div className="flex flex-col md:flex-row justify-between items-center gap-4 text-sm text-muted-foreground">
        <p>
          &copy; {new Date().getFullYear()} Persistent Temp Mail. All rights reserved.
        </p>
        <div className="flex gap-6">
          <a href="#" className="hover:text-foreground transition-colors">Privacy Policy</a>
          <a href="#" className="hover:text-foreground transition-colors">Terms of Service</a>
          <a href="#" className="hover:text-foreground transition-colors">Contact</a>
        </div>
      </div>
    </footer>
  );
}
