import axios from "axios";
import { ENVARS } from "@/lib/envars";

// Tạo instance axios với config mặc định
const http = axios.create({
    baseURL: ENVARS.NEXT_PUBLIC_SERVER_URL,
    timeout: 10000,
    withCredentials: true,
    headers: {
        "Content-Type": "application/json",
        "Accept": "application/json",
    },
});

http.interceptors.response.use(
    (response) => {
        return response.data;
    },
    (error) => {
        const message = error.response?.data?.error || error.response?.data?.message || error.message;

        const customError: any = new Error(message);
        customError.status = error.response?.status;
        customError.body = error.response?.data;

        return Promise.reject(customError);
    }
);

export default http;