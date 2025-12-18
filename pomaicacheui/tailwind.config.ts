import type { Config } from "tailwindcss"

const config = {
    darkMode: "class",
    content: [
        './pages/**/*.{ts,tsx}',
        './components/**/*.{ts,tsx}',
        './app/**/*.{ts,tsx}',
        './src/**/*.{ts,tsx}',
    ],
    theme: {
        container: {
            center: true,
            padding: "2rem",
            screens: { "2xl": "1400px" },
        },
        extend: {
            colors: {
                // --- POMAI DESIGN SYSTEM (Source of Truth) ---
                pomai: {
                    dark: "#0B1120",       // Deep Navy/Black (Nền chính)
                    card: "#151F32",       // Card background
                    red: "#E14D58",        // Pomegranate Red (Primary Action)
                    gold: "#D4AF37",       // Circuit Gold (Focus/Accent)
                    blue: "#4B8B8B",       // Tech Blue (Secondary)
                    text: "#F9F7F2",       // Off-white text (Dễ đọc trên nền tối)
                    "text-muted": "#94A3B8",
                },
                border: "hsl(var(--border))",
                input: "hsl(var(--input))",
                ring: "hsl(var(--ring))",
                background: "hsl(var(--background))",
                foreground: "hsl(var(--foreground))",
            },
            backgroundImage: {
                // Gradient đặc trưng
                'pomai-gradient': 'linear-gradient(135deg, #0B1120 0%, #111827 100%)',
                // Hiệu ứng mạch điện (giả lập bằng radial gradient)
                'pomai-circuit': "radial-gradient(circle at 50% 50%, rgba(75, 139, 139, 0.05) 0%, transparent 70%)",
                'pomai-glow': "conic-gradient(from 180deg at 50% 50%, #E14D58 0deg, #D4AF37 180deg, #E14D58 360deg)",
            },
            animation: {
                "spin-slow": "spin 3s linear infinite",
                "pulse-slow": "pulse 4s cubic-bezier(0.4, 0, 0.6, 1) infinite",
            },
        },
    },
    plugins: [require("tailwindcss-animate")],
} satisfies Config

export default config