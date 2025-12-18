import * as z from "zod";

export const signinSchema = z.object({
    email: z.string().min(1, "Email is required").email("Invalid email address"),
    password: z.string().min(6, "Password must be at least 6 characters"),
});

export const signupSchema = z.object({
    displayName: z.string().min(2, "Name must be at least 2 characters"),
    email: z.string().min(1, "Email is required").email("Invalid email address"),
    password: z.string().min(6, "Password must be at least 6 characters"),
    confirmPassword: z.string().min(1, "Confirm password is required"),
}).refine((data) => data.password === data.confirmPassword, {
    message: "Passwords do not match",
    path: ["confirmPassword"],
});

export const verifySchema = z.object({
    email: z.string().email("Invalid email address"),
    code: z.string().min(6, "Verification code must be 6 characters"),
});

export type SigninValues = z.infer<typeof signinSchema>;
export type SignupValues = z.infer<typeof signupSchema>;
export type VerifyValues = z.infer<typeof verifySchema>;