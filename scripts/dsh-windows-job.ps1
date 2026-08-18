param(
  [Parameter(Mandatory = $true)]
  [string]$Payload
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

Add-Type -TypeDefinition @'
using System;
using System.ComponentModel;
using System.Runtime.InteropServices;
using System.Text;

public static class AnbanDshWindowsJob
{
    private const uint CREATE_SUSPENDED = 0x00000004;
    private const uint CREATE_UNICODE_ENVIRONMENT = 0x00000400;
    private const uint EXTENDED_STARTUPINFO_PRESENT = 0x00080000;
    private const uint JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE = 0x00002000;
    private const int JobObjectExtendedLimitInformation = 9;
    private const int ERROR_INSUFFICIENT_BUFFER = 122;
    private const uint STARTF_USESTDHANDLES = 0x00000100;
    private const uint WAIT_OBJECT_0 = 0x00000000;
    private const uint INFINITE = 0xffffffff;
    private const int STD_INPUT_HANDLE = -10;
    private const int STD_OUTPUT_HANDLE = -11;
    private const int STD_ERROR_HANDLE = -12;
    private static readonly UIntPtr PROC_THREAD_ATTRIBUTE_JOB_LIST =
        new UIntPtr(0x0002000Du);

    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
    private struct STARTUPINFO
    {
        public int cb;
        public string lpReserved;
        public string lpDesktop;
        public string lpTitle;
        public int dwX;
        public int dwY;
        public int dwXSize;
        public int dwYSize;
        public int dwXCountChars;
        public int dwYCountChars;
        public int dwFillAttribute;
        public uint dwFlags;
        public short wShowWindow;
        public short cbReserved2;
        public IntPtr lpReserved2;
        public IntPtr hStdInput;
        public IntPtr hStdOutput;
        public IntPtr hStdError;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct STARTUPINFOEX
    {
        public STARTUPINFO StartupInfo;
        public IntPtr lpAttributeList;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct PROCESS_INFORMATION
    {
        public IntPtr hProcess;
        public IntPtr hThread;
        public uint dwProcessId;
        public uint dwThreadId;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct JOBOBJECT_BASIC_LIMIT_INFORMATION
    {
        public long PerProcessUserTimeLimit;
        public long PerJobUserTimeLimit;
        public uint LimitFlags;
        public UIntPtr MinimumWorkingSetSize;
        public UIntPtr MaximumWorkingSetSize;
        public uint ActiveProcessLimit;
        public UIntPtr Affinity;
        public uint PriorityClass;
        public uint SchedulingClass;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct IO_COUNTERS
    {
        public ulong ReadOperationCount;
        public ulong WriteOperationCount;
        public ulong OtherOperationCount;
        public ulong ReadTransferCount;
        public ulong WriteTransferCount;
        public ulong OtherTransferCount;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct JOBOBJECT_EXTENDED_LIMIT_INFORMATION
    {
        public JOBOBJECT_BASIC_LIMIT_INFORMATION BasicLimitInformation;
        public IO_COUNTERS IoInfo;
        public UIntPtr ProcessMemoryLimit;
        public UIntPtr JobMemoryLimit;
        public UIntPtr PeakProcessMemoryUsed;
        public UIntPtr PeakJobMemoryUsed;
    }

    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern IntPtr CreateJobObject(IntPtr jobAttributes, string name);

    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern bool SetInformationJobObject(
        IntPtr job,
        int informationClass,
        IntPtr information,
        uint informationLength);

    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern bool CreateProcessW(
        string applicationName,
        StringBuilder commandLine,
        IntPtr processAttributes,
        IntPtr threadAttributes,
        bool inheritHandles,
        uint creationFlags,
        IntPtr environment,
        string currentDirectory,
        ref STARTUPINFOEX startupInfo,
        out PROCESS_INFORMATION processInformation);

    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern bool InitializeProcThreadAttributeList(
        IntPtr attributeList,
        int attributeCount,
        uint flags,
        ref UIntPtr size);

    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern bool UpdateProcThreadAttribute(
        IntPtr attributeList,
        uint flags,
        UIntPtr attribute,
        IntPtr value,
        UIntPtr size,
        IntPtr previousValue,
        IntPtr returnSize);

    [DllImport("kernel32.dll")]
    private static extern void DeleteProcThreadAttributeList(IntPtr attributeList);

    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern uint ResumeThread(IntPtr thread);

    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern uint WaitForSingleObject(IntPtr handle, uint milliseconds);

    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern bool GetExitCodeProcess(IntPtr process, out uint exitCode);

    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern bool TerminateProcess(IntPtr process, uint exitCode);

    [DllImport("kernel32.dll")]
    private static extern IntPtr GetStdHandle(int standardHandle);

    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern bool CloseHandle(IntPtr handle);

    private static void ThrowLastError(string operation)
    {
        throw new Win32Exception(Marshal.GetLastWin32Error(), operation);
    }

    private static string QuoteArgument(string argument)
    {
        if (argument.Length > 0 && argument.IndexOfAny(new[] { ' ', '\t', '\n', '\v', '"' }) < 0)
        {
            return argument;
        }

        var result = new StringBuilder();
        result.Append('"');
        var backslashes = 0;
        foreach (var character in argument)
        {
            if (character == '\\')
            {
                backslashes += 1;
                continue;
            }
            if (character == '"')
            {
                result.Append('\\', backslashes * 2 + 1);
                result.Append('"');
                backslashes = 0;
                continue;
            }
            result.Append('\\', backslashes);
            backslashes = 0;
            result.Append(character);
        }
        result.Append('\\', backslashes * 2);
        result.Append('"');
        return result.ToString();
    }

    private static string BuildCommandLine(string command, string[] arguments)
    {
        var result = new StringBuilder(QuoteArgument(command));
        foreach (var argument in arguments)
        {
            result.Append(' ');
            result.Append(QuoteArgument(argument));
        }
        return result.ToString();
    }

    public static int Run(string command, string[] arguments)
    {
        var job = CreateJobObject(IntPtr.Zero, null);
        if (job == IntPtr.Zero)
        {
            ThrowLastError("CreateJobObject");
        }

        var information = new JOBOBJECT_EXTENDED_LIMIT_INFORMATION();
        information.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE;
        var informationLength = Marshal.SizeOf(typeof(JOBOBJECT_EXTENDED_LIMIT_INFORMATION));
        var informationPointer = Marshal.AllocHGlobal(informationLength);
        var attributeList = IntPtr.Zero;
        var jobList = IntPtr.Zero;
        var attributeListInitialized = false;
        PROCESS_INFORMATION process = new PROCESS_INFORMATION();
        try
        {
            Marshal.StructureToPtr(information, informationPointer, false);
            if (!SetInformationJobObject(
                job,
                JobObjectExtendedLimitInformation,
                informationPointer,
                (uint)informationLength))
            {
                ThrowLastError("SetInformationJobObject");
            }

            var attributeListSize = UIntPtr.Zero;
            InitializeProcThreadAttributeList(
                IntPtr.Zero,
                1,
                0,
                ref attributeListSize);
            if (
                Marshal.GetLastWin32Error() != ERROR_INSUFFICIENT_BUFFER ||
                attributeListSize == UIntPtr.Zero)
            {
                ThrowLastError("InitializeProcThreadAttributeList(size)");
            }
            attributeList = Marshal.AllocHGlobal(
                checked((int)attributeListSize.ToUInt64()));
            if (!InitializeProcThreadAttributeList(
                attributeList,
                1,
                0,
                ref attributeListSize))
            {
                ThrowLastError("InitializeProcThreadAttributeList");
            }
            attributeListInitialized = true;

            jobList = Marshal.AllocHGlobal(IntPtr.Size);
            Marshal.WriteIntPtr(jobList, job);
            if (!UpdateProcThreadAttribute(
                attributeList,
                0,
                PROC_THREAD_ATTRIBUTE_JOB_LIST,
                jobList,
                new UIntPtr((uint)IntPtr.Size),
                IntPtr.Zero,
                IntPtr.Zero))
            {
                ThrowLastError("UpdateProcThreadAttribute(JOB_LIST)");
            }

            var startup = new STARTUPINFOEX();
            startup.StartupInfo.cb = Marshal.SizeOf(typeof(STARTUPINFOEX));
            startup.StartupInfo.dwFlags = STARTF_USESTDHANDLES;
            startup.StartupInfo.hStdInput = GetStdHandle(STD_INPUT_HANDLE);
            startup.StartupInfo.hStdOutput = GetStdHandle(STD_OUTPUT_HANDLE);
            startup.StartupInfo.hStdError = GetStdHandle(STD_ERROR_HANDLE);
            startup.lpAttributeList = attributeList;
            var commandLine = new StringBuilder(BuildCommandLine(command, arguments));
            if (!CreateProcessW(
                command,
                commandLine,
                IntPtr.Zero,
                IntPtr.Zero,
                true,
                CREATE_SUSPENDED |
                    CREATE_UNICODE_ENVIRONMENT |
                    EXTENDED_STARTUPINFO_PRESENT,
                IntPtr.Zero,
                null,
                ref startup,
                out process))
            {
                ThrowLastError("CreateProcess");
            }

            try
            {
                if (ResumeThread(process.hThread) == UInt32.MaxValue)
                {
                    ThrowLastError("ResumeThread");
                }
                if (WaitForSingleObject(process.hProcess, INFINITE) != WAIT_OBJECT_0)
                {
                    ThrowLastError("WaitForSingleObject");
                }
                uint exitCode;
                if (!GetExitCodeProcess(process.hProcess, out exitCode))
                {
                    ThrowLastError("GetExitCodeProcess");
                }
                return unchecked((int)exitCode);
            }
            catch
            {
                if (process.hProcess != IntPtr.Zero)
                {
                    TerminateProcess(process.hProcess, 1);
                }
                throw;
            }
            finally
            {
                if (process.hThread != IntPtr.Zero)
                {
                    CloseHandle(process.hThread);
                }
                if (process.hProcess != IntPtr.Zero)
                {
                    CloseHandle(process.hProcess);
                }
            }
        }
        finally
        {
            if (attributeListInitialized)
            {
                DeleteProcThreadAttributeList(attributeList);
            }
            if (jobList != IntPtr.Zero)
            {
                Marshal.FreeHGlobal(jobList);
            }
            if (attributeList != IntPtr.Zero)
            {
                Marshal.FreeHGlobal(attributeList);
            }
            Marshal.FreeHGlobal(informationPointer);
            CloseHandle(job);
        }
    }
}
'@

$decoded = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($Payload))
$specification = ConvertFrom-Json -InputObject $decoded
if ($null -eq $specification.command -or $null -eq $specification.args) {
  throw 'Windows Job Object payload is invalid'
}
$arguments = @($specification.args | ForEach-Object { [string]$_ })
$exitCode = [AnbanDshWindowsJob]::Run(
  [string]$specification.command,
  [string[]]$arguments
)
exit $exitCode
