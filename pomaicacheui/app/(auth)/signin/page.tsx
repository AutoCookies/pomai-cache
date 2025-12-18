import { SigninForm } from "@/components/auth/SigninForm";

export default function SigninPage() {
    return (
        // Container chính: Nền tối, căn giữa
        <div className="min-h-screen w-full flex items-center justify-center bg-pomai-dark relative overflow-hidden">

            {/* Background Decor */}
            <div className="absolute inset-0 bg-pomai-circuit opacity-10 pointer-events-none" />
            <div className="absolute top-0 left-1/4 w-96 h-96 bg-pomai-red/10 rounded-full blur-[128px]" />
            <div className="absolute bottom-0 right-1/4 w-96 h-96 bg-pomai-gold/5 rounded-full blur-[128px]" />

            {/* Card chứa Form */}
            <div className="relative z-10 w-full max-w-[450px] p-4">
                <div className="rounded-xl border border-border bg-pomai-card/50 backdrop-blur-xl shadow-2xl p-8">

                    {/* Header: Logo & Title */}
                    <div className="flex flex-col items-center space-y-2 mb-8 text-center">
                        <div className="h-12 w-12 rounded-lg bg-gradient-to-br from-pomai-red to-pomai-gold flex items-center justify-center shadow-lg shadow-pomai-red/20 mb-2">
                            {/* Thay div này bằng thẻ <Image /> logo của bạn */}
                            <div className="h-6 w-6 bg-pomai-dark rounded-full" />
                        </div>
                        <h1 className="text-2xl font-bold tracking-tight text-white">
                            Pomai <span className="text-pomai-gold">Cache</span>
                        </h1>
                        <p className="text-sm text-muted-foreground">
                            Cổng truy cập an toàn
                        </p>
                    </div>

                    {/* Nhúng Form vào đây */}
                    <SigninForm />
                </div>

                <div className="mt-6 text-center">
                    <p className="text-[10px] text-muted-foreground/50 font-mono uppercase tracking-widest">
                        System Status: <span className="text-green-500">Operational</span>
                    </p>
                </div>
            </div>
        </div>
    );
}