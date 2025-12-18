// lib/auth/AuthContext.tsx
import React, { createContext, useEffect, useReducer, ReactNode } from 'react';
import { authReducer, initialState } from "@/reducers/authReducers"; // Đảm bảo đường dẫn đúng
import { AuthState, User } from '@/lib/auth/types'; // Đảm bảo đường dẫn đúng
import authService from '@/services/authServices';

// ... (Interface AuthContextType giữ nguyên) ...
interface AuthContextType extends AuthState {
    login: (params: any) => Promise<void>;
    register: (params: any) => Promise<void>;
    verify: (params: any) => Promise<void>;
    logout: () => Promise<void>;
    resend: (email: string) => Promise<void>;
    refreshProfile: () => Promise<void>;
    clearError: () => void;
}

export const AuthContext = createContext<AuthContextType | undefined>(undefined);

export const AuthProvider = ({ children }: { children: ReactNode }) => {
    const [state, dispatch] = useReducer(authReducer, initialState);

    // 1. Check Login status on Mount
    useEffect(() => {
        const initAuth = async () => {
            try {
                // response bây giờ chính là User object (nhờ TypeScript cast ở service)
                const user = await authService.me();

                dispatch({
                    type: 'INITIALIZE',
                    payload: { user },
                });
            } catch (error) {
                dispatch({
                    type: 'INITIALIZE',
                    payload: { user: null },
                });
            }
        };

        initAuth();
    }, []);

    // 2. Actions
    const login = async (params: any) => {
        dispatch({ type: 'LOGIN_START' });
        try {
            const res = await authService.signin(params);
            // res là AuthResponse { user, accessToken... }
            dispatch({ type: 'LOGIN_SUCCESS', payload: { user: res.user } });
        } catch (error: any) {
            const msg = error.message || 'Login failed';
            dispatch({ type: 'LOGIN_FAILURE', payload: { error: msg } });
            throw error;
        }
    };

    const register = async (params: any) => {
        dispatch({ type: 'LOGIN_START' });
        try {
            await authService.signup(params);
            dispatch({ type: 'CLEAR_ERROR' });
        } catch (error: any) {
            const msg = error.message || 'Signup failed';
            dispatch({ type: 'LOGIN_FAILURE', payload: { error: msg } });
            throw error;
        }
    };

    const verify = async (params: any) => {
        dispatch({ type: 'LOGIN_START' });
        try {
            const res = await authService.verifyEmail(params);
            dispatch({ type: 'LOGIN_SUCCESS', payload: { user: res.user } });
        } catch (error: any) {
            const msg = error.message || 'Verification failed';
            dispatch({ type: 'LOGIN_FAILURE', payload: { error: msg } });
            throw error;
        }
    };

    const logout = async () => {
        try {
            await authService.signout();
        } catch (error) {
            console.error("Logout error", error);
        } finally {
            dispatch({ type: 'LOGOUT' });
        }
    };

    // ... (Các hàm resend, refreshProfile, clearError giữ nguyên) ...
    const resend = async (email: string) => {
        try {
            await authService.resendVerification(email);
        } catch (error) {
            throw error;
        }
    };

    const refreshProfile = async () => {
        try {
            const user = await authService.me();
            dispatch({ type: 'LOGIN_SUCCESS', payload: { user } });
        } catch (error) {
            dispatch({ type: 'LOGOUT' });
        }
    }

    const clearError = () => dispatch({ type: 'CLEAR_ERROR' });

    return (
        <AuthContext.Provider
            value={{
                ...state,
                login,
                register,
                verify,
                logout,
                resend,
                refreshProfile,
                clearError,
            }}
        >
            {children}
        </AuthContext.Provider>
    );
};