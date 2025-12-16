/**
 * authReducer.ts
 *
 * Reducer + action types for auth state in the UI.
 * State shape:
 * {
 *   initializing: boolean,  // true while initial /me check or refresh is in progress
 *   user: null | any,       // user object or claims
 *   loading: boolean,       // for explicit actions (signin, signout)
 *   error?: string|null
 * }
 *
 * Actions:
 *  - INIT_START / INIT_DONE
 *  - SET_USER
 *  - CLEAR_USER
 *  - SET_LOADING
 *  - SET_ERROR
 */

export type AuthState = {
    initializing: boolean;
    user: any | null;
    loading: boolean;
    error: string | null;
};

export const initialAuthState: AuthState = {
    initializing: true,
    user: null,
    loading: false,
    error: null
};

export type AuthAction =
    | { type: "INIT_START" }
    | { type: "INIT_DONE" }
    | { type: "SET_USER"; payload: any }
    | { type: "CLEAR_USER" }
    | { type: "SET_LOADING"; payload: boolean }
    | { type: "SET_ERROR"; payload: string | null };

export function authReducer(state: AuthState, action: AuthAction): AuthState {
    switch (action.type) {
        case "INIT_START":
            return { ...state, initializing: true, loading: true, error: null };
        case "INIT_DONE":
            return { ...state, initializing: false, loading: false };
        case "SET_USER":
            return { ...state, user: action.payload, loading: false, error: null, initializing: false };
        case "CLEAR_USER":
            return { ...state, user: null, loading: false, error: null, initializing: false };
        case "SET_LOADING":
            return { ...state, loading: action.payload };
        case "SET_ERROR":
            return { ...state, error: action.payload, loading: false };
        default:
            return state;
    }
}