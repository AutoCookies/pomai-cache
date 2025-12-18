import http from "@/lib/http";
import { AUTH_ENDPOINTS } from "@/lib/auth/authApi";
// Import User type từ file types của bạn để dùng chung
import { User } from "@/lib/auth/types";

// --- Request Params Types ---
interface SignupParams {
    email: string;
    password: string;
    displayName: string;
}

interface VerifyParams {
    email: string;
    code: string;
}

interface SigninParams {
    email: string;
    password: string;
}

// --- Response Types ---
// Response cho Login/Verify/Signup (khớp với Go Backend AuthResponse)
interface AuthResponse {
    user: User;
    accessToken: string;
    refreshToken: string;
}

// --- Auth Functions ---

// Dùng "as Promise<...>" để báo cho TS biết Interceptor đã xử lý response

export const signup = (data: SignupParams) => {
    return http.post(AUTH_ENDPOINTS.SIGNUP, data) as Promise<{ user: User, verificationSent: boolean }>;
};

export const verifyEmail = (data: VerifyParams) => {
    return http.post(AUTH_ENDPOINTS.VERIFY_EMAIL, data) as Promise<AuthResponse>;
};

export const signin = (data: SigninParams) => {
    return http.post(AUTH_ENDPOINTS.SIGNIN, data) as Promise<AuthResponse>;
};

export const me = () => {
    // Backend Go trả về trực tiếp User struct, không bọc trong { user: ... }
    return http.get(AUTH_ENDPOINTS.ME) as Promise<User>;
};

export const refresh = (refreshToken?: string) => {
    const body = refreshToken ? { refreshToken } : {};
    return http.post(AUTH_ENDPOINTS.REFRESH, body) as Promise<{ accessToken: string, refreshToken: string }>;
};

export const signout = (refreshToken?: string) => {
    const body = refreshToken ? { refreshToken } : {};
    return http.post(AUTH_ENDPOINTS.SIGNOUT, body) as Promise<{ ok: boolean }>;
};

export const resendVerification = (email: string) => {
    return http.post(AUTH_ENDPOINTS.RESEND_VERIFICATION, { email }) as Promise<{ ok: boolean, verificationSent: boolean }>;
};

export default {
    signup,
    verifyEmail,
    signin,
    me,
    refresh,
    signout,
    resendVerification
};