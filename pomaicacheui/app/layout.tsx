// This is a server component by default; it can include client components (Providers) as children.
import "./globals.css";
import Providers from "./provider"; // client component

export const metadata = {
  title: "Pomai Cache UI",
  description: "Admin dashboard"
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        {/* Providers is a client component that wraps the app and provides AuthContext */}
        <Providers>
          {children}
        </Providers>
      </body>
    </html>
  );
}