import { ENVARS } from "@/lib/envars";

export const authApi = {
    SIGNIN: `${ENVARS.NEXT_PUBLIC_SERVER_URL}/auth/signin`,
    SIGNUP: `${ENVARS.NEXT_PUBLIC_SERVER_URL}/auth/signup`,
    SIGNOUT: `${ENVARS.NEXT_PUBLIC_SERVER_URL}/auth/signout`,
    ME: `${ENVARS.NEXT_PUBLIC_SERVER_URL}/auth/me`,
    REFRESH: `${ENVARS.NEXT_PUBLIC_SERVER_URL}/auth/refresh`,
    VERIFY_EMAIL: `${ENVARS.NEXT_PUBLIC_SERVER_URL}/auth/verify-email`,
    RESEND_VERIFICATION: `${ENVARS.NEXT_PUBLIC_SERVER_URL}/auth/resend-verification`,
}