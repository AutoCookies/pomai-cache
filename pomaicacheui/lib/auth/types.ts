export interface User {
    id: string;
    email: string;
    displayName: string;
    roles: string[];
    disabled: boolean;
    createdAt?: string;
    updatedAt?: string;
}

export interface AuthState {
    user: User | null;
    isAuthenticated: boolean;
    isLoading: boolean; // Dùng cho lần load đầu tiên (check me)
    isOperating: boolean; // Dùng cho các hành động login/register/verify...
    error: string | null;
}

export type AuthAction =
    | { type: 'INITIALIZE'; payload: { user: User | null } }
    | { type: 'LOGIN_START' }
    | { type: 'LOGIN_SUCCESS'; payload: { user: User } }
    | { type: 'LOGIN_FAILURE'; payload: { error: string } }
    | { type: 'LOGOUT' }
    | { type: 'CLEAR_ERROR' };