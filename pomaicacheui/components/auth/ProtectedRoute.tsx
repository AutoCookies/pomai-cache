"use client"

import { useAuth } from "@/hooks/useAuth";
import { useRouter, usePathname } from "next/navigation";
import { useEffect } from "react";

export const ProtectedRoute = ({ children }: { children: React.ReactNode }) => {
    const { isAuthenticated, isLoading } = useAuth();
    const router = useRouter();
    const pathname = usePathname();

    useEffect(() => {
        // Chỉ redirect khi đã load xong trạng thái auth và kết quả là chưa login
        if (!isLoading && !isAuthenticated) {
            router.push("/signin");
        }
    }, [isAuthenticated, isLoading, router]);

    // Hiển thị loading trong khi chờ check auth
    if (isLoading) {
        return (
            <div className="min-h-screen flex items-center justify-center bg-pomai-dark text-white">
                Loading system...
            </div>
        );
    }

    return isAuthenticated ? <>{children}</> : null;
};