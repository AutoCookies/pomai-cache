"use client";

import React from "react";
import { AuthProvider } from "@/contexts/AuthContext"; // adjust path if needed

export default function Providers({ children }: { children: React.ReactNode }) {
    return <AuthProvider>{children}</AuthProvider>;
}