import { SignupForm } from "@/components/auth/SignupForm";

export default function SignupPage() {
    return (
        <div className="min-h-screen w-full flex items-center justify-center bg-pomai-dark text-pomai-text relative overflow-hidden selection:bg-pomai-gold/30">

            {/* Background Decor */}
            <div className="absolute inset-0 bg-pomai-gradient z-0"></div>
            <div className="absolute inset-0 bg-pomai-circuit opacity-20 pointer-events-none z-0"></div>

            {/* Orbs: Đảo vị trí chút cho khác Signin */}
            <div className="absolute bottom-[-10%] left-[-10%] w-[500px] h-[500px] bg-pomai-red/10 rounded-full blur-[120px] animate-pulse-slow"></div>
            <div className="absolute top-[-10%] right-[-10%] w-[500px] h-[500px] bg-pomai-gold/5 rounded-full blur-[120px] animate-pulse-slow"></div>

            {/* Main Card */}
            <div className="relative z-10 w-full max-w-[500px] px-6 py-12">
                {/* Border Gradient */}
                <div className="relative rounded-2xl p-[1px] bg-gradient-to-b from-pomai-blue/30 via-pomai-gold/20 to-pomai-red/30 shadow-2xl backdrop-blur-sm">

                    {/* Content */}
                    <div className="bg-pomai-card/90 rounded-2xl overflow-hidden backdrop-blur-xl">

                        <div className="h-1 w-full bg-gradient-to-r from-pomai-red via-pomai-gold to-pomai-blue"></div>

                        <div className="p-8 pt-10">
                            {/* Header */}
                            <div className="flex flex-col items-center mb-8 space-y-2 text-center">
                                <h1 className="text-3xl font-bold tracking-tight text-white">
                                    Join <span className="text-pomai-gold">Pomai</span>
                                </h1>
                                <p className="text-sm text-pomai-text-muted">
                                    Begin Your Story
                                </p>
                            </div>

                            {/* Form */}
                            <SignupForm />
                        </div>
                    </div>
                </div>

                {/* Footer */}
                <div className="mt-8 text-center">
                    <p className="text-xs text-pomai-text-muted/40 font-mono">
                        SECURE REGISTRATION • V2.0.1
                    </p>
                </div>
            </div>
        </div>
    );
}