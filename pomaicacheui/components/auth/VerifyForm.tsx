"use client";

import { useState, useEffect } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useRouter, useSearchParams } from "next/navigation";
import { RiLoader4Line, RiArrowRightLine, RiMailSendLine, RiShieldCheckLine } from "@remixicon/react";

import { useAuth } from "@/hooks/useAuth";
import { verifySchema, VerifyValues } from "@/lib/validations/auth";
import { Button } from "@/components/ui/button";
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { InputOTP, InputOTPGroup, InputOTPSlot } from "@/components/ui/input-otp";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { cn } from "@/lib/utils";

export function VerifyForm() {
    const { verify, resend } = useAuth();
    const router = useRouter();
    const searchParams = useSearchParams();

    // Lấy email từ URL ?email=...
    const emailFromUrl = searchParams.get("email") || "";

    const [isLoading, setIsLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [resendStatus, setResendStatus] = useState<"idle" | "sending" | "sent">("idle");

    const form = useForm<VerifyValues>({
        resolver: zodResolver(verifySchema),
        defaultValues: {
            email: emailFromUrl,
            code: "",
        },
    });

    // Cập nhật email nếu URL thay đổi (trường hợp user tự gõ URL)
    useEffect(() => {
        if (emailFromUrl) {
            form.setValue("email", emailFromUrl);
        }
    }, [emailFromUrl, form]);

    async function onSubmit(data: VerifyValues) {
        setIsLoading(true);
        setError(null);
        try {
            await verify({ email: data.email, code: data.code });
            // Verify xong thường sẽ tự login (nhờ AuthContext set cookie), redirect về dashboard
            router.push("/dashboard");
        } catch (err: any) {
            setError(err.message || "Verification failed. Check your code.");
        } finally {
            setIsLoading(false);
        }
    }

    async function onResend() {
        if (!form.getValues("email")) {
            setError("Email is missing. Please go back to signup.");
            return;
        }

        setResendStatus("sending");
        try {
            await resend(form.getValues("email"));
            setResendStatus("sent");
            // Reset status sau 5s
            setTimeout(() => setResendStatus("idle"), 5000);
        } catch (err: any) {
            setError(err.message || "Failed to resend code.");
            setResendStatus("idle");
        }
    }

    return (
        <div className="grid gap-6">
            <Form {...form}>
                <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-6">

                    {error && (
                        <Alert variant="destructive" className="bg-red-900/10 border-red-900/20 text-red-500">
                            <AlertDescription>{error}</AlertDescription>
                        </Alert>
                    )}

                    {/* Hiển thị Email đang verify (Readonly) */}
                    <div className="text-center">
                        <div className="inline-flex items-center justify-center p-2 bg-secondary/10 rounded-full mb-4">
                            <RiMailSendLine className="w-6 h-6 text-pomai-gold" />
                        </div>
                        <p className="text-sm text-muted-foreground">
                            We've sent a code to <br />
                            <span className="font-medium text-foreground">{emailFromUrl || "your email"}</span>
                        </p>
                    </div>

                    {/* Input OTP */}
                    <FormField
                        control={form.control}
                        name="code"
                        render={({ field }) => (
                            <FormItem className="flex flex-col items-center justify-center">
                                <FormLabel className="sr-only">One-Time Password</FormLabel>
                                <FormControl>
                                    <InputOTP
                                        maxLength={6}
                                        {...field}
                                        containerClassName="gap-2"
                                    >
                                        <InputOTPGroup className="gap-2">
                                            <InputOTPSlot index={0} className="w-10 h-12 border-pomai-blue/30 bg-secondary/20 text-lg rounded-md focus:border-pomai-gold focus:ring-pomai-gold" />
                                            <InputOTPSlot index={1} className="w-10 h-12 border-pomai-blue/30 bg-secondary/20 text-lg rounded-md focus:border-pomai-gold focus:ring-pomai-gold" />
                                            <InputOTPSlot index={2} className="w-10 h-12 border-pomai-blue/30 bg-secondary/20 text-lg rounded-md focus:border-pomai-gold focus:ring-pomai-gold" />
                                            <InputOTPSlot index={3} className="w-10 h-12 border-pomai-blue/30 bg-secondary/20 text-lg rounded-md focus:border-pomai-gold focus:ring-pomai-gold" />
                                            <InputOTPSlot index={4} className="w-10 h-12 border-pomai-blue/30 bg-secondary/20 text-lg rounded-md focus:border-pomai-gold focus:ring-pomai-gold" />
                                            <InputOTPSlot index={5} className="w-10 h-12 border-pomai-blue/30 bg-secondary/20 text-lg rounded-md focus:border-pomai-gold focus:ring-pomai-gold" />
                                        </InputOTPGroup>
                                    </InputOTP>
                                </FormControl>
                                <FormMessage />
                            </FormItem>
                        )}
                    />

                    {/* Submit Button */}
                    <Button
                        type="submit"
                        disabled={isLoading}
                        className={cn(
                            "w-full py-6 font-bold text-white transition-all shadow-lg",
                            "bg-pomai-red hover:bg-pomai-red/90",
                            "shadow-pomai-red/20 hover:shadow-pomai-red/40"
                        )}
                    >
                        {isLoading ? (
                            <RiLoader4Line className="mr-2 h-5 w-5 animate-spin" />
                        ) : (
                            <>
                                Verify & Access <RiShieldCheckLine className="ml-2 h-4 w-4" />
                            </>
                        )}
                    </Button>
                </form>
            </Form>

            {/* Resend Link */}
            <div className="text-center text-sm">
                <p className="text-muted-foreground">
                    Didn't receive the code?{" "}
                    <button
                        onClick={onResend}
                        disabled={resendStatus === "sending" || resendStatus === "sent"}
                        className={cn(
                            "font-medium text-pomai-gold hover:text-pomai-red hover:underline transition-colors focus:outline-none",
                            resendStatus === "sent" && "text-green-500 hover:no-underline cursor-default"
                        )}
                    >
                        {resendStatus === "sending" ? "Sending..." :
                            resendStatus === "sent" ? "Sent!" : "Resend Code"}
                    </button>
                </p>
            </div>

            <div className="flex justify-center">
                <Button
                    variant="link"
                    className="text-xs text-muted-foreground hover:text-white"
                    onClick={() => router.push("/auth/signin")}
                >
                    <RiArrowRightLine className="mr-1 h-3 w-3 rotate-180" /> Back to Login
                </Button>
            </div>
        </div>
    );
}