import { VerifyForm } from "@/components/auth/VerifyForm";
import { Suspense } from "react"; // Cần thiết khi dùng useSearchParams trong Next.js App Router

export default function VerifyPage() {
    return (
        <div className="min-h-screen w-full flex items-center justify-center bg-pomai-dark text-pomai-text relative overflow-hidden selection:bg-pomai-gold/30">

            {/* Background Decor */}
            <div className="absolute inset-0 bg-pomai-gradient z-0"></div>
            <div className="absolute inset-0 bg-pomai-circuit opacity-10 pointer-events-none z-0"></div>

            {/* Glow Effects */}
            <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[600px] h-[600px] bg-pomai-blue/5 rounded-full blur-[120px] pointer-events-none"></div>

            {/* Main Card */}
            <div className="relative z-10 w-full max-w-[450px] px-6">

                {/* Border Gradient Wrapper */}
                <div className="relative rounded-2xl p-[1px] bg-gradient-to-br from-pomai-gold/30 via-pomai-red/20 to-pomai-blue/30 shadow-2xl backdrop-blur-sm">

                    <div className="bg-pomai-card/95 rounded-2xl overflow-hidden backdrop-blur-xl border border-white/5">

                        {/* Top Accent Line */}
                        <div className="h-1 w-full bg-gradient-to-r from-pomai-gold via-pomai-red to-pomai-blue"></div>

                        <div className="p-8 pt-10">
                            {/* Header */}
                            <div className="flex flex-col items-center mb-6 space-y-2 text-center">
                                <h1 className="text-2xl font-bold tracking-tight text-white">
                                    Verify Account
                                </h1>
                                <p className="text-sm text-pomai-text-muted">
                                    Enter the 6-digit code sent to your email
                                </p>
                            </div>

                            {/* Form Wrapped in Suspense */}
                            {/* Suspense là bắt buộc khi dùng useSearchParams ở trang build tĩnh */}
                            <Suspense fallback={<div className="text-center text-sm text-muted-foreground">Loading verification...</div>}>
                                <VerifyForm />
                            </Suspense>
                        </div>
                    </div>
                </div>

                {/* Footer Security Badge */}
                <div className="mt-8 text-center flex justify-center items-center gap-2 text-[10px] text-pomai-text-muted/40 font-mono">
                    <span className="w-2 h-2 rounded-full bg-green-500 animate-pulse"></span>
                    ENCRYPTED CONNECTION
                </div>
            </div>
        </div>
    );
}