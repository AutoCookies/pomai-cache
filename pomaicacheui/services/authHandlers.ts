/**
 * authHandlers.ts
 *
 * Lightweight wrapper around auth HTTP endpoints used by the UI.
 * - Uses fetch with credentials: 'include' so cookies (HttpOnly) are sent/received.
 * - Respects NEXT_PUBLIC_API_BASE if provided (e.g. http://localhost:8080 or blank for same origin).
 *
 * Endpoints used (matching server):
 *  - GET  /auth/me
 *  - POST /auth/refresh      { refreshToken? }  (but cookie-based refresh is default)
 *  - POST /auth/signin       { email, password }
 *  - POST /auth/signout
 *
 * All functions return parsed JSON on success, or throw an error object { status, message }.
 */

import { ENVARS } from "@/lib/envars";
const API_BASE = ENVARS.NEXT_PUBLIC_SERVER_URL;

async function handleJsonResponse(res: Response) {
    const text = await res.text();
    let body: any = {};
    try { body = text ? JSON.parse(text) : {}; } catch { body = { text }; }
    if (!res.ok) {
        const err: any = new Error(body?.error || body?.message || `HTTP ${res.status}`);
        err.status = res.status;
        err.body = body;
        throw err;
    }
    return body;
}

function apiUrl(path: string) {
    if (!API_BASE) return path.startsWith("/") ? path : `/${path}`;
    return `${API_BASE}${path.startsWith("/") ? path : `/${path}`}`;
}

export async function me() {
    const res = await fetch(apiUrl("/auth/me"), {
        method: "GET",
        credentials: "include",
        headers: { "Accept": "application/json" }
    });
    return handleJsonResponse(res);
}

export async function refresh(refreshToken?: string) {
    const body = refreshToken ? { refreshToken } : {};
    const res = await fetch(apiUrl("/auth/refresh"), {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json", "Accept": "application/json" },
        body: Object.keys(body).length ? JSON.stringify(body) : undefined
    });
    return handleJsonResponse(res);
}

export async function signin(email: string, password: string) {
    const res = await fetch(apiUrl("/auth/signin"), {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json", "Accept": "application/json" },
        body: JSON.stringify({ email, password })
    });
    return handleJsonResponse(res);
}

export async function signout() {
    // server may accept POST /auth/signout and clear cookies
    const res = await fetch(apiUrl("/auth/signout"), {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json", "Accept": "application/json" },
        body: JSON.stringify({})
    });
    return handleJsonResponse(res);
}