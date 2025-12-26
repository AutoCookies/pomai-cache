"use client";

import React, { useCallback, useEffect, useState } from "react";
import keyService, { KeyPublic, CreateRotateResp } from "@/services/keyService";
import { formatDbTimestamp } from "@/utils/dateUtils";
import "remixicon/fonts/remixicon.css";

// Type định nghĩa cho hiển thị
type ListKey = KeyPublic & {
    secret?: never;
    name?: string;
    description?: string;
    prefix?: string;
};

// Helper copy
const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
};

export default function ApiKeyPage() {
    const [keys, setKeys] = useState<ListKey[]>([]);
    const [loading, setLoading] = useState(false);
    const [opLoading, setOpLoading] = useState<string | null>(null);
    const [error, setError] = useState<string | null>(null);

    // Modal Create
    const [showCreateModal, setShowCreateModal] = useState(false);
    const [createName, setCreateName] = useState("");
    const [createDescription, setCreateDescription] = useState("");

    // Modal Success (QUAN TRỌNG: Chứa cả ID và Secret để hiển thị)
    const [newlyCreatedKey, setNewlyCreatedKey] = useState<{ secret: string; id: string } | null>(null);

    // Track xem user đang copy trường nào
    const [copiedField, setCopiedField] = useState<string | null>(null);

    // Validate Tool
    const [validateInput, setValidateInput] = useState("");
    const [validateResult, setValidateResult] = useState<{ valid: boolean; msg: string } | null>(null);

    // --- LOGIC ---

    const fetchKeys = useCallback(async () => {
        setLoading(true);
        setError(null);
        try {
            const res = await keyService.listKeys();
            // service trả về res.keys là KeyPublic[], cần ép kiểu nhẹ nếu cấu trúc API thay đổi
            const raw = (res?.keys ?? []) as any[];

            const mapped: ListKey[] = raw.map((k) => ({
                id: k.id ?? k.keyId ?? "",
                tenantId: k.tenant_id ?? k.tenantId,
                createdAt: k.created_at ?? k.createdAt,
                updatedAt: k.updated_at ?? k.updatedAt,
                expiresAt: k.expires_at ?? k.expiresAt,
                isActive: k.is_active ?? k.isActive ?? true,
                name: k.name ?? k.label ?? "Untitled Key",
                description: k.description ?? k.desc,
                prefix: k.prefix ?? k.id?.substring(0, 8),
            }));
            setKeys(mapped);
        } catch (err: any) {
            console.error(err);
            setError("Could not load API Keys. Please try again.");
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        fetchKeys();
    }, [fetchKeys]);

    const handleCreate = async (e: React.FormEvent) => {
        e.preventDefault();
        if (!createName.trim()) return;
        setOpLoading("create");
        setError(null);

        try {
            const res = (await keyService.createKey({
                name: createName.trim(),
                description: createDescription.trim() || undefined,
            })) as CreateRotateResp;

            // 1. Lấy dữ liệu từ Service (có thể undefined do type optional)
            const rawKeyId = res.key_id ?? res.key?.id;
            const secret = res.secret ?? res.key?.secret ?? (res as any).key;

            // 2. Validate dữ liệu: Nếu thiếu 1 trong 2 thì báo lỗi ngay
            if (!secret) throw new Error("Server did not return a Secret Key.");
            if (!rawKeyId) throw new Error("Server did not return a Key ID.");

            // 3. Lúc này TypeScript đã hiểu rawKeyId chắc chắn là string
            const keyId: string = rawKeyId;

            // Lưu cả ID và Secret để hiển thị ở Modal Success
            setNewlyCreatedKey({ secret, id: keyId });
            setShowCreateModal(false);
            setCreateName("");
            setCreateDescription("");

            fetchKeys();
        } catch (err: any) {
            setError(err?.message || "Failed to create key");
        } finally {
            setOpLoading(null);
        }
    };

    const handleRotate = async (keyId: string) => {
        if (!confirm("⚠️ Danger Zone: Rotating this key will invalidate the old secret immediately. Continue?")) return;

        setOpLoading(keyId);
        try {
            const res = (await keyService.rotateKey({ keyId })) as CreateRotateResp;
            const secret = res.secret ?? res.key?.secret ?? (res as any).key;

            if (secret) {
                // keyId ở đây lấy từ tham số hàm (string), nên an toàn tuyệt đối
                setNewlyCreatedKey({ secret, id: keyId });
                fetchKeys();
            }
        } catch (err: any) {
            alert(err?.message || "Failed to rotate key");
        } finally {
            setOpLoading(null);
        }
    };

    const handleDelete = async (keyId: string) => {
        if (!confirm("Are you sure you want to delete this key?")) return;
        setOpLoading(keyId);
        try {
            await keyService.deleteKey(keyId);
            setKeys((prev) => prev.filter((k) => k.id !== keyId));
        } catch (err: any) {
            alert(err?.message || "Failed to delete key");
        } finally {
            setOpLoading(null);
        }
    };

    const handleCopy = (text: string, fieldName: string) => {
        copyToClipboard(text);
        setCopiedField(fieldName);
        setTimeout(() => setCopiedField(null), 2000);
    };

    const handleValidate = async (e: React.FormEvent) => {
        e.preventDefault();
        if (!validateInput.trim()) return;
        setOpLoading("validate");
        setValidateResult(null);
        try {
            const res = await keyService.validateKey({ key: validateInput.trim() });
            setValidateResult({
                valid: !!res.valid,
                msg: res.valid ? "✅ Valid & Active." : "❌ Invalid API Key."
            });
        } catch (err: any) {
            setValidateResult({ valid: false, msg: "❌ Error: " + err.message });
        } finally {
            setOpLoading(null);
        }
    };

    // --- RENDER SUCCESS MODAL (ĐÃ NÂNG CẤP) ---
    const renderSuccessModal = () => {
        if (!newlyCreatedKey) return null;

        // TẠO CHUỖI ĐẦY ĐỦ: ID + SECRET
        const completeKeyString = `${newlyCreatedKey.id}.${newlyCreatedKey.secret}`;

        return (
            <div className="fixed inset-0 z-[60] flex items-center justify-center p-4">
                <div className="absolute inset-0 bg-black/80 backdrop-blur-md transition-opacity" />
                <div className="relative bg-white dark:bg-gray-900 rounded-2xl shadow-2xl w-full max-w-xl p-0 transform transition-all border border-gray-200 dark:border-gray-800 overflow-hidden animate-in fade-in zoom-in-95 duration-200">

                    {/* Header */}
                    <div className="p-6 border-b border-gray-100 dark:border-gray-800 bg-gray-50/50 dark:bg-black/20 text-center">
                        <div className="mx-auto w-12 h-12 bg-green-100 text-green-600 rounded-full flex items-center justify-center mb-3 shadow-sm">
                            <i className="ri-shield-check-line text-2xl" />
                        </div>
                        <h3 className="text-xl font-bold text-gray-900 dark:text-white">Credentials Ready</h3>
                        {/* CẢNH BÁO RÕ RÀNG */}
                        <div className="mt-3 bg-amber-50 dark:bg-amber-900/20 text-amber-800 dark:text-amber-200 text-xs py-2 px-3 rounded-lg border border-amber-100 dark:border-amber-800/50 inline-block">
                            <i className="ri-alert-fill mr-1"></i>
                            <b>Important:</b> A valid API Key must include both the <u>ID</u> and <u>Secret</u>.
                        </div>
                    </div>

                    {/* Main Content */}
                    <div className="p-6">

                        {/* --- PRIMARY COPY: COMPLETE KEY --- */}
                        <div className="mb-6">
                            <div className="flex justify-between items-center mb-2">
                                <label className="text-xs font-extrabold text-black dark:text-white uppercase tracking-wider flex items-center gap-1">
                                    <i className="ri-key-2-fill text-blue-600" /> Complete API Key (ID + Secret)
                                </label>
                                <span className="text-[10px] text-white bg-blue-600 px-2 py-0.5 rounded-full font-bold shadow-sm animate-pulse">
                                    Use This
                                </span>
                            </div>

                            <div className="relative group">
                                <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none">
                                    <i className="ri-links-line text-gray-400"></i>
                                </div>
                                <input
                                    readOnly
                                    value={completeKeyString}
                                    className="block w-full pl-10 pr-24 py-3.5 bg-blue-50/50 dark:bg-blue-900/10 border border-blue-200 dark:border-blue-800 rounded-xl text-sm font-mono text-blue-900 dark:text-blue-100 focus:ring-2 focus:ring-blue-500 outline-none transition-all font-bold"
                                    onClick={(e) => e.currentTarget.select()}
                                />
                                <button
                                    onClick={() => handleCopy(completeKeyString, "complete")}
                                    className={`absolute right-1.5 top-1.5 bottom-1.5 px-4 rounded-lg text-sm font-bold transition-all shadow-sm ${copiedField === "complete"
                                        ? "bg-green-500 text-white"
                                        : "bg-white dark:bg-gray-800 text-black dark:text-white hover:bg-gray-50 dark:hover:bg-gray-700 border border-gray-200 dark:border-gray-600"
                                        }`}
                                >
                                    {copiedField === "complete" ? <><i className="ri-check-line" /> Copied</> : "Copy Key"}
                                </button>
                            </div>
                            <p className="mt-2 text-[11px] text-gray-500 dark:text-gray-400">
                                This string contains your <b>Key ID</b> and <b>Secret</b> joined by a dot. This is the full credential required for the SDK.
                            </p>
                        </div>

                        {/* --- SECONDARY DETAILS (Advanced) --- */}
                        <div className="pt-4 border-t border-dashed border-gray-200 dark:border-gray-800">
                            <div className="flex items-center gap-2 mb-3">
                                <span className="text-xs font-bold text-gray-400 uppercase">Individual Components</span>
                                <div className="h-px bg-gray-100 dark:bg-gray-800 flex-1"></div>
                            </div>

                            <div className="grid grid-cols-1 gap-3">
                                {/* Key ID */}
                                <div className="flex items-center justify-between bg-gray-50 dark:bg-gray-900/50 p-2.5 rounded-lg border border-gray-100 dark:border-gray-800">
                                    <div className="min-w-0 flex-1 mr-3">
                                        <div className="text-[10px] text-gray-400 uppercase font-semibold mb-0.5">Part 1: Key ID</div>
                                        <div className="font-mono text-xs text-gray-600 dark:text-gray-300 truncate">{newlyCreatedKey.id}</div>
                                    </div>
                                    <button onClick={() => handleCopy(newlyCreatedKey.id, "id")} className="p-1.5 text-gray-400 hover:text-black dark:hover:text-white transition-colors">
                                        {copiedField === "id" ? <i className="ri-check-line text-green-500" /> : <i className="ri-file-copy-line" />}
                                    </button>
                                </div>

                                {/* Secret */}
                                <div className="flex items-center justify-between bg-amber-50/30 dark:bg-amber-900/10 p-2.5 rounded-lg border border-amber-100 dark:border-amber-900/20">
                                    <div className="min-w-0 flex-1 mr-3">
                                        <div className="text-[10px] text-amber-600/70 uppercase font-semibold mb-0.5">Part 2: Secret</div>
                                        <div className="font-mono text-xs text-gray-600 dark:text-gray-300 truncate">{newlyCreatedKey.secret}</div>
                                    </div>
                                    <button onClick={() => handleCopy(newlyCreatedKey.secret, "secret")} className="p-1.5 text-gray-400 hover:text-black dark:hover:text-white transition-colors">
                                        {copiedField === "secret" ? <i className="ri-check-line text-green-500" /> : <i className="ri-file-copy-line" />}
                                    </button>
                                </div>
                            </div>
                        </div>

                    </div>

                    {/* Footer */}
                    <div className="p-4 bg-gray-50 dark:bg-gray-900 border-t border-gray-100 dark:border-gray-800 text-center">
                        <button
                            onClick={() => setNewlyCreatedKey(null)}
                            className="w-full py-3 bg-black dark:bg-white text-white dark:text-black rounded-xl font-bold text-sm shadow-lg hover:shadow-xl hover:-translate-y-0.5 transition-all"
                        >
                            I have copied the complete key
                        </button>
                    </div>
                </div>
            </div>
        );
    };

    return (
        <div className="max-w-6xl mx-auto py-10 px-4 sm:px-6">

            {/* Header */}
            <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 mb-8">
                <div>
                    <h1 className="text-3xl font-bold text-gray-900 dark:text-white tracking-tight">API Keys</h1>
                    <p className="text-gray-500 mt-2">Create and manage access keys for your applications.</p>
                </div>
                <button
                    onClick={() => setShowCreateModal(true)}
                    className="inline-flex items-center gap-2 px-5 py-2.5 bg-black dark:bg-white text-white dark:text-black rounded-lg text-sm font-semibold hover:opacity-90 transition-all shadow-lg hover:shadow-xl"
                >
                    <i className="ri-add-line text-lg" /> Create New Key
                </button>
            </div>

            {/* Error Banner */}
            {error && (
                <div className="mb-6 p-4 bg-red-50 text-red-700 text-sm rounded-lg border border-red-100 flex items-center gap-2">
                    <i className="ri-error-warning-fill text-lg" /> {error}
                </div>
            )}

            {/* Main List */}
            <div className="bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-800 rounded-xl shadow-sm overflow-hidden">
                <div className="overflow-x-auto">
                    <table className="w-full text-left text-sm">
                        <thead className="bg-gray-50 dark:bg-gray-800/50 border-b border-gray-100 dark:border-gray-800 text-gray-500 font-medium uppercase text-xs tracking-wider">
                            <tr>
                                <th className="px-6 py-4">Name & ID</th>
                                <th className="px-6 py-4">Status</th>
                                <th className="px-6 py-4">Created</th>
                                <th className="px-6 py-4 text-right">Actions</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-gray-100 dark:divide-gray-800">
                            {loading ? (
                                <tr>
                                    <td colSpan={4} className="px-6 py-12 text-center text-gray-400">
                                        <i className="ri-loader-4-line animate-spin text-2xl mb-2 block" /> Loading keys...
                                    </td>
                                </tr>
                            ) : keys.length === 0 ? (
                                <tr>
                                    <td colSpan={4} className="px-6 py-12 text-center">
                                        <div className="flex flex-col items-center justify-center text-gray-400">
                                            <i className="ri-key-2-line text-4xl mb-3 opacity-20" />
                                            <p className="text-base font-medium text-gray-900 dark:text-white">No API Keys found</p>
                                        </div>
                                    </td>
                                </tr>
                            ) : (
                                keys.map((k) => (
                                    <tr key={k.id} className="group hover:bg-gray-50/50 dark:hover:bg-gray-800/30 transition-colors">
                                        {/* Name & ID */}
                                        <td className="px-6 py-4 align-top max-w-[300px]">
                                            <div className="font-semibold text-gray-900 dark:text-white">{k.name}</div>
                                            {k.description && <div className="text-xs text-gray-500 mt-0.5 truncate">{k.description}</div>}
                                            <div className="mt-1.5 flex items-center gap-1.5">
                                                <span className="text-[10px] text-gray-400 font-bold">ID:</span>
                                                <code className="text-[10px] bg-gray-100 dark:bg-gray-800 px-1.5 py-0.5 rounded text-gray-500 font-mono select-all">{k.id}</code>
                                            </div>
                                        </td>

                                        {/* Status */}
                                        <td className="px-6 py-4 align-middle">
                                            {k.isActive ? (
                                                <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-green-50 text-green-700 border border-green-100">
                                                    <span className="w-1.5 h-1.5 rounded-full bg-green-500 animate-pulse" /> Active
                                                </span>
                                            ) : (
                                                <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-gray-100 text-gray-600 border border-gray-200">Inactive</span>
                                            )}
                                        </td>

                                        {/* Date */}
                                        <td className="px-6 py-4 align-middle text-gray-500 text-xs">
                                            {formatDbTimestamp(k.createdAt, k.createdAt)}
                                        </td>

                                        {/* Actions */}
                                        <td className="px-6 py-4 align-middle text-right">
                                            <div className="flex items-center justify-end gap-2 opacity-0 group-hover:opacity-100 transition-opacity">
                                                <button
                                                    onClick={() => handleRotate(k.id)}
                                                    disabled={!!opLoading}
                                                    className="p-2 rounded-md text-gray-400 hover:text-amber-600 hover:bg-amber-50 transition-colors"
                                                    title="Rotate Key"
                                                >
                                                    <i className={`ri-refresh-line text-lg ${opLoading === k.id ? "animate-spin text-amber-600" : ""}`} />
                                                </button>
                                                <button
                                                    onClick={() => handleDelete(k.id)}
                                                    disabled={!!opLoading}
                                                    className="p-2 rounded-md text-gray-400 hover:text-red-600 hover:bg-red-50 transition-colors"
                                                    title="Delete Key"
                                                >
                                                    <i className="ri-delete-bin-line text-lg" />
                                                </button>
                                            </div>
                                        </td>
                                    </tr>
                                ))
                            )}
                        </tbody>
                    </table>
                </div>
            </div>

            {/* Validation Tool */}
            <div className="mt-10 pt-8 border-t border-gray-100 dark:border-gray-800">
                <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-4">Validate API Key</h3>
                <div className="bg-gray-50 dark:bg-gray-900/50 p-4 rounded-xl border border-dashed border-gray-300 dark:border-gray-700 max-w-2xl">
                    <form onSubmit={handleValidate} className="flex gap-2">
                        <input
                            type="password"
                            value={validateInput}
                            onChange={(e) => {
                                setValidateInput(e.target.value);
                                setValidateResult(null);
                            }}
                            className="flex-1 px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-black text-sm font-mono placeholder:font-sans"
                            // Thay đổi Placeholder để nhắc user
                            placeholder="Paste complete key here (format: id.secret)..."
                        />
                        <button
                            disabled={!validateInput.trim() || opLoading === "validate"}
                            type="submit"
                            className="px-4 py-2 bg-gray-200 dark:bg-gray-800 text-gray-900 dark:text-white rounded-lg hover:bg-gray-300 dark:hover:bg-gray-700 text-sm font-medium transition-colors"
                        >
                            {opLoading === "validate" ? <i className="ri-loader-4-line animate-spin" /> : "Check"}
                        </button>
                    </form>
                    {validateResult && (
                        <div className={`mt-3 text-sm flex items-center gap-2 ${validateResult.valid ? "text-green-600" : "text-red-600"}`}>
                            <i className={`text-lg ${validateResult.valid ? "ri-checkbox-circle-fill" : "ri-close-circle-fill"}`} />
                            <span className="font-medium">{validateResult.msg}</span>
                        </div>
                    )}
                </div>
            </div>

            {/* Create Modal */}
            {showCreateModal && (
                <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
                    <div className="absolute inset-0 bg-black/60 backdrop-blur-sm transition-opacity" onClick={() => setShowCreateModal(false)} />
                    <div className="relative bg-white dark:bg-gray-900 rounded-2xl shadow-2xl w-full max-w-md p-6 animate-in fade-in zoom-in-95 duration-200">
                        <div className="flex justify-between items-center mb-5">
                            <h3 className="text-xl font-bold text-gray-900 dark:text-white">Create New API Key</h3>
                            <button onClick={() => setShowCreateModal(false)} className="text-gray-400 hover:text-gray-600"><i className="ri-close-line text-2xl" /></button>
                        </div>
                        <form onSubmit={handleCreate} className="space-y-4">
                            <div>
                                <label className="block text-xs font-bold text-gray-700 dark:text-gray-300 uppercase tracking-wide mb-1.5">Name</label>
                                <input
                                    autoFocus
                                    value={createName}
                                    onChange={(e) => setCreateName(e.target.value)}
                                    className="w-full px-4 py-2.5 rounded-lg border border-gray-300 dark:border-gray-700 bg-transparent text-sm focus:ring-2 focus:ring-black dark:focus:ring-white focus:border-transparent outline-none transition-shadow"
                                    placeholder="e.g. My Application"
                                />
                            </div>
                            <div>
                                <label className="block text-xs font-bold text-gray-700 dark:text-gray-300 uppercase tracking-wide mb-1.5">Description (Optional)</label>
                                <textarea
                                    value={createDescription}
                                    onChange={(e) => setCreateDescription(e.target.value)}
                                    className="w-full px-4 py-2.5 rounded-lg border border-gray-300 dark:border-gray-700 bg-transparent text-sm focus:ring-2 focus:ring-black dark:focus:ring-white outline-none h-24 resize-none transition-shadow"
                                    placeholder="Key usage details..."
                                />
                            </div>
                            <div className="pt-4 flex justify-end gap-3">
                                <button type="button" onClick={() => setShowCreateModal(false)} className="px-5 py-2.5 text-sm font-medium text-gray-600 hover:bg-gray-100 rounded-lg transition-colors">
                                    Cancel
                                </button>
                                <button type="submit" disabled={!createName.trim() || opLoading === "create"} className="px-5 py-2.5 text-sm font-bold bg-black dark:bg-white text-white dark:text-black rounded-lg hover:opacity-90 disabled:opacity-50 shadow-md transition-all">
                                    {opLoading === "create" ? "Generating..." : "Create Key"}
                                </button>
                            </div>
                        </form>
                    </div>
                </div>
            )}

            {/* Success Modal */}
            {renderSuccessModal()}

        </div>
    );
}