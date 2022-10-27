using System;
using System.IO;
using NineDigit.WixSharpExtensions;
using WixSharp;
using WixSharp.CommonTasks;
using File = WixSharp.File;

namespace WixSetup.Datadog
{
    public class AgentInstaller : IWixProjectEvents
    {
        // Company
        private const string CompanyFullName = "Datadog, inc.";

        // Product
        private const string ProductFullName = "Datadog Agent";
        private const string ProductDescription = "Datadog helps you monitor your infrastructure and application";
        private const string ProductHelpUrl = @"https://help.datadoghq.com/hc/en-us";
        private const string ProductAboutUrl = @"https://www.datadoghq.com/about/";
        private const string ProductComment = @"Copyright 2015 - Present Datadog";
        private const string ProductContact = @"https://www.datadoghq.com/about/contact/";

        // same value for all versions; must not be changed
        private static readonly Guid ProductUpgradeCode = new Guid("0c50421b-aefb-4f15-a809-7af256d608a5");
        private static readonly string ProductLicenceRtfFilePath = Path.Combine("assets", "LICENSE.rtf");
        private static readonly string ProductIconFilePath = Path.Combine("assets", "project.ico");
        private static readonly string InstallerBackgroundImagePath = Path.Combine("assets", "dialog_background.bmp");
        private static readonly string InstallerBannerImagePath = Path.Combine("assets", "banner_background.bmp");

        // Source directories
        private const string InstallerSource = @"C:\opt\datadog-agent";
        private const string BinSource = @"C:\omnibus-ruby\src\datadog-agent\src\github.com\DataDog\datadog-agent\bin";
        private const string EtcSource = @"C:\omnibus-ruby\src\etc\datadog-agent";

        private readonly AgentBinaries _agentBinaries;
        private readonly AgentFeatures _agentFeatures = new AgentFeatures();
        private readonly AgentPython _agentPython = new AgentPython();
        private readonly AgentVersion _agentVersion = new AgentVersion();
        private readonly AgentSignature _agentSignature;
        private readonly AgentCustomActions _agentCustomActions = new AgentCustomActions();
        private readonly AgentInstallerUI _agentInstallerUi;

        public AgentInstaller()
        {
            _agentBinaries = new AgentBinaries(BinSource);
            _agentSignature = new AgentSignature(this, _agentPython, _agentBinaries);
            _agentInstallerUi = new AgentInstallerUI(this, _agentCustomActions);
        }

        public Project ConfigureProject()
        {
            var project = new Project("Datadog Agent",
                new User(new Id("ddagentuser"), "[DDAGENTUSER_NAME]")
                {
                    Domain = "[DDAGENTUSER_DOMAIN]",
                    Password = "[DDAGENTUSER_PASSWORD]",
                    PasswordNeverExpires = true,
                    RemoveOnUninstall = true,
                    //ComponentCondition = Condition.NOT("DDAGENTUSER_FOUND=\"true\")")
                },
                _agentCustomActions.ReadConfig,
                _agentCustomActions.WriteConfig,
                _agentCustomActions.ProcessDdAgentUserCredentials,
                _agentCustomActions.DecompressPythonDistributions,
                _agentCustomActions.ConfigureUser,
                new Property("APIKEY")
                {
                    AttributesDefinition = "Hidden=yes;Secure=yes"
                },
                //new Property("DDAGENTUSER_NAME", "ddagentuser"),
                new Property("DDAGENTUSER_PASSWORD")
                {
                    AttributesDefinition = "Hidden=yes;Secure=yes"
                },
                new Property("PROJECTLOCATION",
                    new RegistrySearch(
                        RegistryHive.LocalMachine,
                        "SOFTWARE\\Datadog\\Datadog Agent",
                        "InstallPath", RegistrySearchType.raw)
                )
                {
                    AttributesDefinition = "Secure=yes"
                },
                new Property("APPLICATIONDATADIRECTORY",
                    new RegistrySearch(
                        RegistryHive.LocalMachine,
                        "SOFTWARE\\Datadog\\Datadog Agent",
                        "ConfigRoot",
                        RegistrySearchType.raw)
                )
                {
                    AttributesDefinition = "Secure=yes"
                },
                new CloseApplication(new Id("CloseTrayApp"),
                    Path.GetFileName(_agentBinaries.Tray),
                    closeMessage: true,
                    rebootPrompt: false
                )
                {
                    Timeout = 1
                },
                new RegKey(
                    _agentFeatures.MainApplication,
                    RegistryHive.LocalMachine, @"Software\Datadog\Datadog Agent",
                    new RegValue("InstallPath", "[PROJECTLOCATION]") { Permissions = new[] { new Permission { User = "[DDAGENTUSER_DOMAIN]\\[DDAGENTUSER_NAME]", GenericAll = true } } },
                    new RegValue("ConfigRoot", "[APPLICATIONDATADIRECTORY]") { Permissions = new[] { new Permission { User = "[DDAGENTUSER_DOMAIN]\\[DDAGENTUSER_NAME]", GenericAll = true } } }
                )
                {
                    Win64 = true
                },
                new RemoveRegistryKey(_agentFeatures.MainApplication, @"Software\Datadog\Datadog Agent")
            )
            .SetProjectInfo(
                upgradeCode: ProductUpgradeCode,
                name: ProductFullName,
                description: ProductDescription,
                // SetProjectInfo throws an Exception is Revision is != 0
                // we use Revision = 2 for the next gen installer while it's still a prototype
                version: new Version(_agentVersion.Version.Major, _agentVersion.Version.Minor,
                    _agentVersion.Version.Build, 0)
            )
            .SetControlPanelInfo(
                name: ProductFullName,
                manufacturer: CompanyFullName,
                readme: ProductHelpUrl,
                comment: ProductComment,
                contact: ProductContact,
                helpUrl: new Uri(ProductHelpUrl),
                aboutUrl: new Uri(ProductAboutUrl),
                productIconFilePath: new FileInfo(ProductIconFilePath)
            )
            .DisableDowngradeToPreviousVersion()
            .SetMinimalUI(
                backgroundImage: new FileInfo(InstallerBackgroundImagePath),
                bannerImage: new FileInfo(InstallerBannerImagePath),
                // $@"{installerSource}\LICENSE" is not RTF and Compiler.AllowNonRtfLicense = true doesn't help.
                licenceRtfFile: new FileInfo(ProductLicenceRtfFilePath)
            )
            .AddDirectories(
                CreateProgramFilesFolder(),
                new Dir(new Id("APPLICATIONDATADIRECTORY"), @"%CommonAppData%\Datadog",
                    new DirPermission("[WIX_ACCOUNT_ADMINISTRATORS]", GenericPermission.All) { ChangePermission = true },
                    new DirPermission("[WIX_ACCOUNT_LOCALSYSTEM]", GenericPermission.All) { ChangePermission = true },
                    new DirPermission("[WIX_ACCOUNT_USERS]", GenericPermission.None) { ChangePermission = true },
                    new DirPermission("[DDAGENTUSER_DOMAIN]\\[DDAGENTUSER_NAME]", GenericPermission.Read | GenericPermission.Execute) { ChangePermission = true },
                    new DirFiles($@"{EtcSource}\*.yaml.example"),
                    new Dir("checks.d"),
                    new Dir(new Id("EXAMPLECONFSLOCATION"), "conf.d",
                        new Files($@"{EtcSource}\extra_package_files\EXAMPLECONFSLOCATION\*")
                    )
                ),
                new Dir(@"%ProgramMenu%\Datadog",
                    new ExeFileShortcut
                    {
                        Name = "Datadog Agent Manager",
                        Target = "[AGENT]ddtray.exe",
                        Arguments = "&quot;-launch-gui&quot;",
                        WorkingDirectory = "AGENT",
                    }
                ),
                new Dir("logs")
            )
            // enable the ability to repair the installation even when the original MSI is no longer available.
            //.EnableResilientPackage() // Resilient package requires a .Net version newer than what is installed on 2008 R2
            ;

            project.Platform = Platform.x64;
            // MSI 4.0+ required
            project.InstallerVersion = 400;
            project.DefaultFeature = _agentFeatures.MainApplication;
            project.Codepage = "1252";
            project.InstallPrivileges = InstallPrivileges.elevated;
            project.LocalizationFile = "localization-en-us.wxl";
            project.OutFileName = $"datadog-agent-{_agentVersion.PackageVersion}-{_agentVersion.Version.Revision}-x86_64";
            project.DigitalSignature = _agentSignature.Signature;

            // clear default media as we will add it via MediaTemplate
            project.Media.Clear();
            project.WixSourceGenerated += document =>
            {
                if (WixSourceGenerated != null)
                {
                    WixSourceGenerated(document);
                }
                document.Select("Wix/Product")
                    .AddElement("MediaTemplate", "CabinetTemplate=cab{0}.cab; CompressionLevel=high; EmbedCab=yes; MaximumUncompressedMediaSize=2");
            };
            project.WixSourceFormated += (ref string content) => WixSourceFormated?.Invoke(content);
            project.WixSourceSaved += name => WixSourceSaved?.Invoke(name);

            project.UI = WUI.WixUI_Common;
            project.CustomUI = _agentInstallerUi.CustomUI;
            project.AddXmlInclude("dialogs/apikeydlg.wxi")
                   .AddXmlInclude("dialogs/sitedlg.wxi");

            return project;
        }

        private Dir CreateProgramFilesFolder()
        {
            var targetBinFolder = CreateBinFolder();
            var binFolder =
                new Dir(new Id("APPLICATIONROOTDIRECTORY"), @"%ProgramFiles%\Datadog",
                    new Dir(new Id("PROJECTLOCATION"), "Datadog Agent",
                        targetBinFolder,
                        new Dir("LICENSES",
                            new Files($@"{InstallerSource}\LICENSES\*")
                            ),
                        new DirFiles($@"{InstallerSource}\*.json"),
                        new DirFiles($@"{InstallerSource}\*.txt"),
                        new CompressedDir(this, "embedded3", $@"{InstallerSource}\embedded3")
                        )
                    );
            if (_agentPython.IncludePython2)
            {
                binFolder.AddFile(new CompressedDir(this, "embedded2", $@"{InstallerSource}\embedded3"));
            }
            return binFolder;
        }

        private static PermissionEx DefaultPermissions()
        {
            return new PermissionEx
            {
                User = "Everyone",
                ServicePauseContinue = true,
                ServiceQueryStatus = true,
                ServiceStart = true,
                ServiceStop = true,
                ServiceUserDefinedControl = true
            };
        }

        private static ServiceInstaller GenerateServiceInstaller(string name, string displayName, string description)
        {
            return new ServiceInstaller
            {
                Id = new Id("ddagentservice"),
                Name = name,
                DisplayName = displayName,
                Description = description,
                StartOn = null,
                Start = SvcStartType.auto,
                DelayedAutoStart = false,
                RemoveOn = SvcEvent.Uninstall_Wait,
                ServiceSid = ServiceSid.none,
                FirstFailureActionType = FailureActionType.restart,
                SecondFailureActionType = FailureActionType.restart,
                ThirdFailureActionType = FailureActionType.restart,
                RestartServiceDelayInSeconds = 60,
                ResetPeriodInDays = 0,
                PreShutdownDelay = 1000 * 60 * 3,
                PermissionEx = DefaultPermissions(),
                Account = "[DDAGENTUSER_DOMAIN]\\[DDAGENTUSER_NAME]",
                Password = "[DDAGENTUSER_PASSWORD]"
            };
        }

        private static ServiceInstaller GenerateDependentServiceInstaller(Id id, string name, string displayName, string description, string account, string password = null)
        {
            return new ServiceInstaller
            {
                Id = id,
                Name = name,
                DisplayName = displayName,
                Description = description,
                StartOn = null,
                Start = SvcStartType.demand,
                RemoveOn = SvcEvent.Uninstall_Wait,
                ServiceSid = ServiceSid.none,
                FirstFailureActionType = FailureActionType.restart,
                SecondFailureActionType = FailureActionType.restart,
                ThirdFailureActionType = FailureActionType.restart,
                RestartServiceDelayInSeconds = 60,
                ResetPeriodInDays = 0,
                PreShutdownDelay = 1000 * 60 * 3,
                PermissionEx = DefaultPermissions(),
                Interactive = false,
                Type = SvcType.ownProcess,
                Account = account,
                Password = password,
                DependsOn = new[]
                {
                    new ServiceDependency("datadogagent")
                }
            };
        }

        private Dir CreateBinFolder()
        {
            var agentService = GenerateServiceInstaller("datadogagent", "Datadog Agent", "Send metrics to Datadog");
            var processAgentService = GenerateDependentServiceInstaller(new Id("ddagentprocessservice"), "datadog-process-agent", "Datadog Process Agent", "Send process metrics to Datadog", "LocalSystem");
            var traceAgentService = GenerateDependentServiceInstaller(new Id("ddagenttraceservice"), "datadog-trace-agent", "Datadog Trace Agent", "Send tracing metrics to Datadog", "[DDAGENTUSER_DOMAIN]\\[DDAGENTUSER_NAME]", "[DDAGENTUSER_PASSWORD]");
            var systemProbeService = GenerateDependentServiceInstaller(new Id("ddagentsysprobeservice"), "datadog-system-probe", "Datadog System Probe", "Send network metrics to Datadog", "LocalSystem");

            var targetBinFolder = new Dir("bin",
                new File(_agentBinaries.Agent, agentService),
                new EventSource
                {
                    Name = "DatadogAgent",
                    Log = "Application",
                    EventMessageFile = $"[BIN]{Path.GetFileName(_agentBinaries.Agent)}",
                    AttributesDefinition = "SupportsErrors=yes; SupportsInformationals=yes; SupportsWarnings=yes"
                },
                new File(_agentBinaries.LibDatadogAgentThree),
                new Dir("agent",
                    new Dir("dist",
                        new Files($@"{InstallerSource}\bin\agent\dist\*")
                    ),
                    new Dir("driver",
                        new Merge(_agentFeatures.Npm, $@"{BinSource}\agent\DDNPM.msm")
                        {
                            Feature = _agentFeatures.Npm
                        }
                    ),
                    new File(_agentBinaries.Tray),
                    new File(_agentBinaries.ProcessAgent, processAgentService),
                    new EventSource
                    {
                        Name = "datadog-process-agent",
                        Log = "Application",
                        EventMessageFile = $"[BIN]{Path.GetFileName(_agentBinaries.ProcessAgent)}",
                        AttributesDefinition = "SupportsErrors=yes; SupportsInformationals=yes; SupportsWarnings=yes"
                    },
                    new File(_agentBinaries.SecurityAgent),
                    new File(_agentBinaries.SystemProbe, systemProbeService),
                    new EventSource
                    {
                        Name = "datadog-system-probe",
                        Log = "Application",
                        EventMessageFile = $"[BIN]{Path.GetFileName(_agentBinaries.SystemProbe)}",
                        AttributesDefinition = "SupportsErrors=yes; SupportsInformationals=yes; SupportsWarnings=yes"
                    },
                    new File(_agentBinaries.TraceAgent, traceAgentService),
                    new EventSource
                    {
                        Name = "datadog-trace-agent",
                        Log = "Application",
                        EventMessageFile = $"[BIN]{Path.GetFileName(_agentBinaries.TraceAgent)}",
                        AttributesDefinition = "SupportsErrors=yes; SupportsInformationals=yes; SupportsWarnings=yes"
                    }
                )
            );
            if (_agentPython.IncludePython2)
            {
                targetBinFolder.AddFile(new File(_agentBinaries.LibDatadogAgentTwo));
            };
            return targetBinFolder;
        }

        public event XDocumentGeneratedDlgt WixSourceGenerated;
        public event XDocumentSavedDlgt WixSourceSaved;
        public event XDocumentFormatedDlgt WixSourceFormated;
    }
}