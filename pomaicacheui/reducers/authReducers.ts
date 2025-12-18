// lib/auth/authReducer.ts
import { AuthState, AuthAction } from "@/lib/auth/types";

export const initialState: AuthState = {
    user: null,
    isAuthenticated: false,
    isLoading: true,      // Mặc định loading true để chờ check /me
    isOperating: false,   // Loading khi ấn nút login
    error: null,
};

export function authReducer(state: AuthState, action: AuthAction): AuthState {
    switch (action.type) {
        case 'INITIALIZE':
            return {
                ...state,
                isAuthenticated: !!action.payload.user,
                user: action.payload.user,
                isLoading: false,
            };

        case 'LOGIN_START':
            return {
                ...state,
                isOperating: true,
                error: null,
            };

        case 'LOGIN_SUCCESS':
            return {
                ...state,
                isAuthenticated: true,
                user: action.payload.user,
                isOperating: false,
                error: null,
            };

        case 'LOGIN_FAILURE':
            return {
                ...state,
                isAuthenticated: false,
                user: null,
                isOperating: false,
                error: action.payload.error,
            };

        case 'LOGOUT':
            return {
                ...state,
                isAuthenticated: false,
                user: null,
                error: null,
            };

        case 'CLEAR_ERROR':
            return {
                ...state,
                error: null,
            };

        default:
            return state;
    }
}