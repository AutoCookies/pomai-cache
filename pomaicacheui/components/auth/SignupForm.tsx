"use client";

import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useRouter } from "next/navigation";
import Link from "next/link";
// Remix Icons
import {
    RiLoader4Line,
    RiLockPasswordLine,
    RiMailLine,
    RiUserLine,
    RiArrowRightLine,
    RiShieldCheckLine
} from "@remixicon/react";

import { useAuth } from "@/hooks/useAuth";
import { signupSchema, SignupValues } from "@/lib/validations/auth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { cn } from "@/lib/utils";

export function SignupForm() {
    const { register: registerUser } = useAuth(); // Đổi tên để tránh trùng với register của react-hook-form
    const router = useRouter();
    const [isLoading, setIsLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const form = useForm<SignupValues>({
        resolver: zodResolver(signupSchema),
        defaultValues: {
            displayName: "",
            email: "",
            password: "",
            confirmPassword: "",
        },
    });

    async function onSubmit(data: SignupValues) {
        setIsLoading(true);
        setError(null);
        try {
            await registerUser({
                email: data.email,
                password: data.password,
                displayName: data.displayName
            });
            // Chuyển hướng sang trang Verify kèm email để tiện nhập liệu
            router.push(`/verify-email?email=${encodeURIComponent(data.email)}`);
        } catch (err: any) {
            setError(err.message || "Registration failed. Please try again.");
        } finally {
            setIsLoading(false);
        }
    }

    return (
        <div className="grid gap-6">
            <Form {...form}>
                <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">

                    {error && (
                        <Alert variant="destructive" className="bg-red-900/10 border-red-900/20 text-red-500">
                            <AlertDescription>{error}</AlertDescription>
                        </Alert>
                    )}

                    {/* Display Name */}
                    <FormField
                        control={form.control}
                        name="displayName"
                        render={({ field }) => (
                            <FormItem>
                                <FormLabel className="text-xs uppercase text-muted-foreground font-semibold tracking-wider">Display Name</FormLabel>
                                <FormControl>
                                    <div className="relative">
                                        <RiUserLine className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
                                        <Input
                                            placeholder="Pomai Explorer"
                                            className="pl-9 bg-secondary/20 border-border focus-visible:ring-pomai-gold focus-visible:border-pomai-gold transition-all"
                                            {...field}
                                        />
                                    </div>
                                </FormControl>
                                <FormMessage />
                            </FormItem>
                        )}
                    />

                    {/* Email */}
                    <FormField
                        control={form.control}
                        name="email"
                        render={({ field }) => (
                            <FormItem>
                                <FormLabel className="text-xs uppercase text-muted-foreground font-semibold tracking-wider">Email</FormLabel>
                                <FormControl>
                                    <div className="relative">
                                        <RiMailLine className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
                                        <Input
                                            placeholder="user@pomai.cache"
                                            className="pl-9 bg-secondary/20 border-border focus-visible:ring-pomai-gold focus-visible:border-pomai-gold transition-all"
                                            {...field}
                                        />
                                    </div>
                                </FormControl>
                                <FormMessage />
                            </FormItem>
                        )}
                    />

                    {/* Password & Confirm Row */}
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <FormField
                            control={form.control}
                            name="password"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel className="text-xs uppercase text-muted-foreground font-semibold tracking-wider">Password</FormLabel>
                                    <FormControl>
                                        <div className="relative">
                                            <RiLockPasswordLine className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
                                            <Input
                                                type="password"
                                                placeholder="••••••"
                                                className="pl-9 bg-secondary/20 border-border focus-visible:ring-pomai-gold focus-visible:border-pomai-gold transition-all"
                                                {...field}
                                            />
                                        </div>
                                    </FormControl>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />

                        <FormField
                            control={form.control}
                            name="confirmPassword"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel className="text-xs uppercase text-muted-foreground font-semibold tracking-wider">Confirm</FormLabel>
                                    <FormControl>
                                        <div className="relative">
                                            <RiShieldCheckLine className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
                                            <Input
                                                type="password"
                                                placeholder="••••••"
                                                className="pl-9 bg-secondary/20 border-border focus-visible:ring-pomai-gold focus-visible:border-pomai-gold transition-all"
                                                {...field}
                                            />
                                        </div>
                                    </FormControl>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />
                    </div>

                    <Button
                        type="submit"
                        disabled={isLoading}
                        className={cn(
                            "w-full py-6 font-bold text-white transition-all shadow-lg mt-2",
                            "bg-pomai-red hover:bg-pomai-red/90",
                            "shadow-pomai-red/20 hover:shadow-pomai-red/40"
                        )}
                    >
                        {isLoading ? (
                            <RiLoader4Line className="mr-2 h-5 w-5 animate-spin" />
                        ) : (
                            <>
                                Initialize Account <RiArrowRightLine className="ml-2 h-4 w-4" />
                            </>
                        )}
                    </Button>
                </form>
            </Form>

            <div className="text-center text-sm text-muted-foreground">
                Already have an account?{" "}
                <Link href="/signin" className="font-medium text-pomai-gold hover:text-pomai-red hover:underline transition-colors">
                    Access System
                </Link>
            </div>
        </div>
    );
}