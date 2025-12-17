"use client";

import React, { createContext, useContext, useEffect, useReducer, useCallback } from "react";
import * as authAPI from "@/services/authHandlers"; // adjust path if necessary
import { authReducer, initialAuthState, AuthState } from "@/reducers/authReducers"; // adjust path

type AuthContextValue = {
    state: AuthState;
    signin: (email: string, password: string) => Promise<any>;
    signout: () => Promise<void>;
    refresh: () => Promise<any>;
    reload: () => Promise<any>;
};

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

export const AuthProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
    const [state, dispatch] = useReducer(authReducer, initialAuthState);

    const initialize = useCallback(async () => {
        dispatch({ type: "INIT_START" });
        try {
            try {
                const res = await authAPI.me();
                const user = res.user ?? res.claims ?? res;
                dispatch({ type: "SET_USER", payload: user });
            } catch (meErr: any) {
                if (meErr?.status === 401) {
                    try {
                        await authAPI.refresh();
                        const res2 = await authAPI.me();
                        const user = res2.user ?? res2.claims ?? res2;
                        dispatch({ type: "SET_USER", payload: user });
                    } catch {
                        dispatch({ type: "CLEAR_USER" });
                    }
                } else {
                    dispatch({ type: "CLEAR_USER" });
                    dispatch({ type: "SET_ERROR", payload: meErr?.message ?? String(meErr) });
                }
            }
        } finally {
            dispatch({ type: "INIT_DONE" });
        }
    }, []);

    useEffect(() => {
        initialize();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    const signin = useCallback(async (email: string, password: string) => {
        dispatch({ type: "SET_LOADING", payload: true });
        try {
            const res = await authAPI.signin(email, password);
            const user = res.user ?? res.claims ?? res;
            dispatch({ type: "SET_USER", payload: user });
            return user;
        } catch (err: any) {
            dispatch({ type: "SET_ERROR", payload: err?.message ?? String(err) });
            throw err;
        }
    }, []);

    const signout = useCallback(async () => {
        dispatch({ type: "SET_LOADING", payload: true });
        try {
            await authAPI.signout();
        } catch (err) {
            console.warn("signout error", err);
        } finally {
            dispatch({ type: "CLEAR_USER" });
        }
    }, []);

    const refresh = useCallback(async () => {
        dispatch({ type: "SET_LOADING", payload: true });
        try {
            await authAPI.refresh();
            try {
                const meRes = await authAPI.me();
                const user = meRes.user ?? meRes.claims ?? meRes;
                dispatch({ type: "SET_USER", payload: user });
                return user;
            } catch {
                dispatch({ type: "CLEAR_USER" });
                return null;
            }
        } catch (err: any) {
            dispatch({ type: "SET_ERROR", payload: err?.message ?? String(err) });
            dispatch({ type: "CLEAR_USER" });
            throw err;
        }
    }, []);

    const reload = useCallback(async () => {
        try {
            const res = await authAPI.me();
            const user = res.user ?? res.claims ?? res;
            dispatch({ type: "SET_USER", payload: user });
            return user;
        } catch (err) {
            dispatch({ type: "CLEAR_USER" });
            throw err;
        }
    }, []);

    const value: AuthContextValue = {
        state,
        signin,
        signout,
        refresh,
        reload
    };

    return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
};

export function useAuth() {
    const ctx = useContext(AuthContext);
    if (!ctx) throw new Error("useAuth must be used within AuthProvider");
    return ctx;
}